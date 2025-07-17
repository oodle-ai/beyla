package ebpfcommon

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/v2/bson"
)

var requests = expirable.NewLRU[MongoRequestKey, *MongoRequestValue](1000, nil, 0)

const (
	StartTime     = 1000
	EndTime       = 2000
	MessageLength = 65
	PreBodyLength = 21 // 16 for header + 5 for flags and section type
	RequestID     = 1
)

func getConnInfo() bpfConnectionInfoT {
	return bpfConnectionInfoT{
		S_addr: [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 192, 168, 0, 1},
		D_addr: [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 8, 8, 8, 8},
		S_port: 27017,
		D_port: 27017,
	}
}

var (
	defaultRequestData  = bson.D{bson.E{Key: commFind, Value: "my_collection"}, bson.E{Key: "$db", Value: "my_db"}}
	defaultResponseData = bson.D{bson.E{Key: "ok", Value: 1.0}}
)

func getRequestPayload(hdr *msgHeader, flags uint32, section SectionType, body *bson.D) []byte {
	if body == nil {
		body = &defaultRequestData
	}
	bsonBytes, _ := bson.Marshal(*body)
	if hdr == nil {
		hdr = &msgHeader{
			MessageLength: PreBodyLength + int32(len(bsonBytes)),
			RequestID:     RequestID,
			ResponseTo:    0,
			OpCode:        opMsg,
		}
	}
	byteBuffer := new(bytes.Buffer)
	_ = binary.Write(byteBuffer, binary.LittleEndian, hdr)
	_ = binary.Write(byteBuffer, binary.LittleEndian, flags) // empty flags
	_ = binary.Write(byteBuffer, binary.LittleEndian, section)
	_ = binary.Write(byteBuffer, binary.LittleEndian, bsonBytes)
	return byteBuffer.Bytes()
}

func getResponsePayload(hdr *msgHeader, flags uint32, section SectionType, data *bson.D) []byte {
	if data == nil {
		data = &defaultResponseData
	}
	bsonBytes, _ := bson.Marshal(*data)
	if hdr == nil {
		hdr = &msgHeader{
			MessageLength: PreBodyLength + int32(len(bsonBytes)),
			RequestID:     RequestID + 1,
			ResponseTo:    RequestID,
			OpCode:        opMsg,
		}
	}
	byteBuffer := new(bytes.Buffer)
	_ = binary.Write(byteBuffer, binary.LittleEndian, hdr)
	_ = binary.Write(byteBuffer, binary.LittleEndian, flags) // empty flags
	_ = binary.Write(byteBuffer, binary.LittleEndian, section)
	_ = binary.Write(byteBuffer, binary.LittleEndian, bsonBytes)
	return byteBuffer.Bytes()
}

func TestProcessMongoEventFailIfPayloadShorterThenHeader(t *testing.T) {
	defer requests.Purge()
	connInfo := getConnInfo()
	_, _, err := ProcessMongoEvent([]uint8{0x00, 0x00, 0x00, 0x00}, StartTime, EndTime, connInfo, requests)
	assert.Error(t, err, "Expected error for short buffer")
}

func TestProcessMongoEventFailIfHdrMessageLengthLessThenHeaderLength(t *testing.T) {
	defer requests.Purge()
	connInfo := getConnInfo()
	shortHdr := msgHeader{
		MessageLength: 3,
		RequestID:     RequestID,
		ResponseTo:    0,
		OpCode:        opMsg,
	}
	payload := getRequestPayload(&shortHdr, 0, sectionTypeBody, nil)
	_, _, err := ProcessMongoEvent(payload, StartTime, EndTime, connInfo, requests)
	assert.Error(t, err, "Expected error for message length less than header length")
}

func TestProcessMongoEventFailOnUnknownOp(t *testing.T) {
	defer requests.Purge()
	connInfo := getConnInfo()
	invalidOpHdr := msgHeader{
		MessageLength: MessageLength,
		RequestID:     RequestID,
		ResponseTo:    0,
		OpCode:        42,
	}
	payload := getRequestPayload(&invalidOpHdr, 0, sectionTypeBody, nil)
	_, _, err := ProcessMongoEvent(payload, StartTime, EndTime, connInfo, requests)
	assert.Error(t, err, "Expected error for unknown opcode")
}

func TestProcessMongoEventFailOnInvalidFlags(t *testing.T) {
	defer requests.Purge()
	connInfo := getConnInfo()
	payload := getRequestPayload(nil, 0|0x08, sectionTypeBody, nil)
	_, _, err := ProcessMongoEvent(payload, StartTime, EndTime, connInfo, requests)
	assert.Error(t, err, "Expected error for invalid flags")
}

func TestProcessMongoEventFailOnInvalidSectionType(t *testing.T) {
	defer requests.Purge()
	connInfo := getConnInfo()
	payload := getRequestPayload(nil, 0|0x08, 6, nil)
	_, _, err := ProcessMongoEvent(payload, StartTime, EndTime, connInfo, requests)
	assert.Error(t, err, "Expected error for invalid section type")
}

func TestProcessMongoEventFailIfResponseHasNoMatchingRequest(t *testing.T) {
	defer requests.Purge()
	connInfo := getConnInfo()
	payload := getResponsePayload(nil, 0, sectionTypeBody, nil)
	_, _, err := ProcessMongoEvent(payload, StartTime, EndTime, connInfo, requests)
	assert.Error(t, err, "Expected error response without matching request")
}

func TestProcessMongoEventFailIfResponseToDoesNotMatchRequest(t *testing.T) {
	defer requests.Purge()
	connInfo := getConnInfo()
	requestPayload := getRequestPayload(nil, 0, sectionTypeBody, nil)
	_, moreToCome, err := ProcessMongoEvent(requestPayload, StartTime, EndTime, connInfo, requests)
	require.NoError(t, err, "Expected no error for valid MongoDB event")
	assert.True(t, moreToCome)
	response := bson.D{bson.E{Key: "ok", Value: 1.0}}
	responsePayload := getResponsePayload(&msgHeader{
		MessageLength: PreBodyLength + int32(len(response)),
		RequestID:     RequestID + 1,
		ResponseTo:    RequestID + 5,
		OpCode:        opMsg,
	}, 0, sectionTypeBody, &response)
	// send the same request again, the connection should be expecting a response
	_, _, err = ProcessMongoEvent(responsePayload, StartTime, EndTime, connInfo, requests)
	assert.Error(t, err, "Expected error, responseTo does not match request ID")
}

func TestProcessMongoEventFailNoAdditionalRequestIfNoMoreToComeInRequest(t *testing.T) {
	defer requests.Purge()
	connInfo := getConnInfo()
	payload := getRequestPayload(nil, 0, sectionTypeBody, nil)
	_, moreToCome, err := ProcessMongoEvent(payload, StartTime, EndTime, connInfo, requests)
	require.NoError(t, err, "Expected no error for valid MongoDB event")
	assert.True(t, moreToCome)

	// send the same request again, the connection should be expecting a response
	_, _, err = ProcessMongoEvent(payload, StartTime, EndTime, connInfo, requests)
	assert.Error(t, err, "Expected error when not expecting more request data but receiving it")
}

func TestProcessMongoEventFailExpectsMoreRequestToComeButGotResponse(t *testing.T) {
	defer requests.Purge()
	connInfo := getConnInfo()
	requestPayload := getRequestPayload(nil, 0|flagMoreToCome, sectionTypeBody, nil)
	_, moreToCome, err := ProcessMongoEvent(requestPayload, StartTime, EndTime, connInfo, requests)
	require.NoError(t, err, "Expected no error for valid MongoDB event")
	assert.True(t, moreToCome)

	responsePayload := getResponsePayload(nil, 0, sectionTypeBody, nil)
	// send the same request again, the connection should be expecting a response
	_, _, err = ProcessMongoEvent(responsePayload, StartTime, EndTime, connInfo, requests)
	assert.Error(t, err, "Expected error when not expecting more request data but receiving it")
}

func TestProcessMongoEventFailIfMultiResponseSentButNoExhaustAllowed(t *testing.T) {
	defer requests.Purge()
	connInfo := getConnInfo()
	requestPayload := getRequestPayload(nil, 0, sectionTypeBody, nil)
	_, moreToCome, err := ProcessMongoEvent(requestPayload, StartTime, EndTime, connInfo, requests)
	require.NoError(t, err, "Expected no error for valid MongoDB event")
	assert.True(t, moreToCome)

	responsePayload := getResponsePayload(nil, 0|flagMoreToCome, sectionTypeBody, nil)
	_, _, err = ProcessMongoEvent(responsePayload, StartTime, EndTime, connInfo, requests)
	assert.Error(t, err, "Expected error if multi-response sent but no exhaust allowed")
}

func TestProcessMongoEventFailSendRequestAfterResponse(t *testing.T) {
	defer requests.Purge()
	connInfo := getConnInfo()
	requestPayload := getRequestPayload(nil, 0|flagExhaustAllowed, sectionTypeBody, nil)
	_, moreToCome, err := ProcessMongoEvent(requestPayload, StartTime, EndTime, connInfo, requests)
	require.NoError(t, err, "Expected no error for valid MongoDB event")
	assert.True(t, moreToCome)

	responsePayload := getResponsePayload(nil, 0|flagMoreToCome, sectionTypeBody, nil)
	_, moreToCome, err = ProcessMongoEvent(responsePayload, StartTime, EndTime, connInfo, requests)
	require.NoError(t, err, "Expected no error for valid MongoDB event")
	assert.True(t, moreToCome)
	// send the same request again, the connection should be expecting a response
	_, _, err = ProcessMongoEvent(requestPayload, StartTime, EndTime, connInfo, requests)
	assert.Error(t, err, "Expected error when sending request after response")
}

func TestProcessMongoEventSuccessParsingSingleRequestResponse(t *testing.T) {
	defer requests.Purge()
	connInfo := getConnInfo()
	requestPayload := getRequestPayload(nil, 0, sectionTypeBody, nil)
	_, moreToCome, err := ProcessMongoEvent(requestPayload, StartTime, EndTime, connInfo, requests)
	require.NoError(t, err, "Expected no error for valid MongoDB event")
	assert.True(t, moreToCome)

	responsePayload := getResponsePayload(nil, 0, sectionTypeBody, nil)
	// send the same request again, the connection should be expecting a response
	mongoRequestValue, moreToCome, err := ProcessMongoEvent(responsePayload, StartTime, EndTime, connInfo, requests)
	require.NoError(t, err, "Expected no error for valid MongoDB event")
	assert.False(t, moreToCome, "Expected no more data to come after response")
	assert.NotNil(t, mongoRequestValue, "Expected MongoRequestValue to be returned")
	assert.Len(t, mongoRequestValue.RequestSections, 1, "Expected one request section")
	firstRequestSection := mongoRequestValue.RequestSections[0]
	assert.Equal(t, sectionTypeBody, firstRequestSection.Type, "Expected first section type to be sectionTypeBody")
	assert.Equal(t, defaultRequestData, firstRequestSection.Body, "Expected first section body to match request data")
	assert.Len(t, mongoRequestValue.ResponseSections, 1, "Expected one response section")
	firstResponseSection := mongoRequestValue.ResponseSections[0]
	assert.Equal(t, sectionTypeBody, firstResponseSection.Type, "Expected first section type to be sectionTypeBody")
	assert.Equal(t, defaultResponseData, firstResponseSection.Body, "Expected first section body to match request data")
}

func TestProcessMongoEventSuccessParsingMultiRequestSingleResponse(t *testing.T) {
	defer requests.Purge()
	connInfo := getConnInfo()
	insertComm := bson.D{bson.E{Key: commInsert, Value: "my_collection"}, bson.E{Key: "$db", Value: "my_db"}}
	requestPayload := getRequestPayload(nil, 0|flagMoreToCome, sectionTypeBody, &insertComm)
	_, moreToCome, err := ProcessMongoEvent(requestPayload, StartTime, EndTime, connInfo, requests)
	require.NoError(t, err, "Expected no error for valid MongoDB event")
	assert.True(t, moreToCome)
	data := bson.D{bson.E{Key: "Name", Value: "Alice"}}
	dataRequestPayload := getRequestPayload(nil, 0, sectionTypeDocumentSequence, &data)
	_, moreToCome, err = ProcessMongoEvent(dataRequestPayload, StartTime, EndTime, connInfo, requests)
	require.NoError(t, err, "Expected no error for valid MongoDB event")
	assert.True(t, moreToCome)

	responsePayload := getResponsePayload(nil, 0, sectionTypeBody, nil)
	// send the same request again, the connection should be expecting a response
	mongoRequestValue, moreToCome, err := ProcessMongoEvent(responsePayload, StartTime, EndTime, connInfo, requests)
	require.NoError(t, err, "Expected no error for valid MongoDB event")
	assert.False(t, moreToCome, "Expected no more data to come after response")
	assert.NotNil(t, mongoRequestValue, "Expected MongoRequestValue to be returned")

	assert.Len(t, mongoRequestValue.RequestSections, 2, "Expected one request section")

	firstRequestSection := mongoRequestValue.RequestSections[0]
	assert.Equal(t, sectionTypeBody, firstRequestSection.Type, "Expected first section type to be sectionTypeBody")
	assert.Equal(t, insertComm, firstRequestSection.Body, "Expected first section body to match request data")

	secondRequestSection := mongoRequestValue.RequestSections[1]
	assert.Equal(t, sectionTypeDocumentSequence, secondRequestSection.Type, "Expected first section type to be sectionTypeBody")

	assert.Len(t, mongoRequestValue.ResponseSections, 1, "Expected one response section")
	firstResponseSection := mongoRequestValue.ResponseSections[0]
	assert.Equal(t, sectionTypeBody, firstResponseSection.Type, "Expected first section type to be sectionTypeBody")
	assert.Equal(t, defaultResponseData, firstResponseSection.Body, "Expected first section body to match request data")
}

func TestProcessMongoEventSuccessParsingSingleRequestMultiResponse(t *testing.T) {
	defer requests.Purge()
	connInfo := getConnInfo()
	requestPayload := getRequestPayload(nil, 0|flagExhaustAllowed, sectionTypeBody, nil)
	_, moreToCome, err := ProcessMongoEvent(requestPayload, StartTime, EndTime, connInfo, requests)
	require.NoError(t, err, "Expected no error for valid MongoDB event")
	assert.True(t, moreToCome)

	responsePayload := getResponsePayload(nil, 0|flagMoreToCome, sectionTypeBody, nil)
	// send the same request again, the connection should be expecting a response
	_, moreToCome, err = ProcessMongoEvent(responsePayload, StartTime, EndTime, connInfo, requests)
	require.NoError(t, err, "Expected no error for valid MongoDB event")
	assert.True(t, moreToCome, "Expected no more data to come after response")

	data := bson.D{bson.E{Key: "Name", Value: "Alice"}}
	dataRequestPayload := getResponsePayload(nil, 0, sectionTypeDocumentSequence, &data)
	mongoRequestValue, moreToCome, err := ProcessMongoEvent(dataRequestPayload, StartTime, EndTime, connInfo, requests)
	require.NoError(t, err, "Expected no error for valid MongoDB event")
	assert.False(t, moreToCome)
	assert.NotNil(t, mongoRequestValue, "Expected MongoRequestValue to be returned")

	assert.Len(t, mongoRequestValue.RequestSections, 1, "Expected one request section")
	requestSection := mongoRequestValue.RequestSections[0]
	assert.Equal(t, sectionTypeBody, requestSection.Type, "Expected first section type to be sectionTypeBody")
	assert.Equal(t, defaultRequestData, requestSection.Body, "Expected first section body to match request data")

	assert.Len(t, mongoRequestValue.ResponseSections, 2, "Expected one response section")

	firstResponseSection := mongoRequestValue.ResponseSections[0]
	assert.Equal(t, sectionTypeBody, firstResponseSection.Type, "Expected first section type to be sectionTypeBody")
	assert.Equal(t, defaultResponseData, firstResponseSection.Body, "Expected first section body to match request data")
	secondResponseSection := mongoRequestValue.ResponseSections[1]
	assert.Equal(t, sectionTypeDocumentSequence, secondResponseSection.Type, "Expected first section type to be sectionTypeBody")
}

func TestProcessMongoEventSuccessWhenResponseOnlyContainsHeader(t *testing.T) {
	defer requests.Purge()
	connInfo := getConnInfo()
	requestPayload := getRequestPayload(nil, 0, sectionTypeBody, nil)
	_, moreToCome, err := ProcessMongoEvent(requestPayload, StartTime, EndTime, connInfo, requests)
	require.NoError(t, err, "Expected no error for valid MongoDB event")
	assert.True(t, moreToCome)

	hdr := &msgHeader{
		MessageLength: PreBodyLength + 50,
		RequestID:     RequestID + 1,
		ResponseTo:    RequestID,
		OpCode:        opMsg,
	}
	// includes only header, no body
	byteBuffer := new(bytes.Buffer)
	_ = binary.Write(byteBuffer, binary.LittleEndian, hdr)

	// send the same request again, the connection should be expecting a response
	mongoRequestValue, moreToCome, err := ProcessMongoEvent(byteBuffer.Bytes(), StartTime, EndTime, connInfo, requests)
	require.NoError(t, err, "Expected no error for valid MongoDB event")
	assert.False(t, moreToCome, "Expected no more data to come after response")
	assert.NotNil(t, mongoRequestValue, "Expected MongoRequestValue to be returned")
	assert.Len(t, mongoRequestValue.RequestSections, 1, "Expected one request section")
	firstRequestSection := mongoRequestValue.RequestSections[0]
	assert.Equal(t, sectionTypeBody, firstRequestSection.Type, "Expected first section type to be sectionTypeBody")
	assert.Equal(t, defaultRequestData, firstRequestSection.Body, "Expected first section body to match request data")
	assert.Empty(t, mongoRequestValue.ResponseSections, "Expected zero response section")
}

func TestProcessMongoEventSuccessWhenCannotParseBsonInRequest(t *testing.T) {
	defer requests.Purge()
	connInfo := getConnInfo()
	bsonBytes, _ := bson.Marshal(defaultRequestData)
	hdr := msgHeader{
		MessageLength: PreBodyLength + int32(len(bsonBytes)),
		RequestID:     RequestID,
		ResponseTo:    0,
		OpCode:        opMsg,
	}
	byteBuffer := new(bytes.Buffer)
	_ = binary.Write(byteBuffer, binary.LittleEndian, hdr)
	_ = binary.Write(byteBuffer, binary.LittleEndian, int32(0))
	_ = binary.Write(byteBuffer, binary.LittleEndian, sectionTypeBody)
	_ = binary.Write(byteBuffer, binary.LittleEndian, bsonBytes[:len(bsonBytes)-4]) // truncate last bytes to make it invalid BSON
	requestPayload := byteBuffer.Bytes()
	_, moreToCome, err := ProcessMongoEvent(requestPayload, StartTime, EndTime, connInfo, requests)
	require.NoError(t, err, "Expected no error for valid MongoDB event")
	assert.True(t, moreToCome)

	responsePayload := getResponsePayload(nil, 0, sectionTypeBody, nil)
	// send the same request again, the connection should be expecting a response
	mongoRequestValue, moreToCome, err := ProcessMongoEvent(responsePayload, StartTime, EndTime, connInfo, requests)
	require.NoError(t, err, "Expected no error for valid MongoDB event")
	assert.False(t, moreToCome, "Expected no more data to come after response")
	assert.NotNil(t, mongoRequestValue, "Expected MongoRequestValue to be returned")
	assert.Len(t, mongoRequestValue.RequestSections, 1, "Expected one request section")
	firstRequestSection := mongoRequestValue.RequestSections[0]
	assert.Equal(t, sectionTypeBody, firstRequestSection.Type, "Expected first section type to be sectionTypeBody")
	// With partial parsing, we should still be able to extract some fields
	assert.True(t, len(firstRequestSection.Body) > 0, "Expected first section body to have some fields from partial parsing")
	assert.Len(t, mongoRequestValue.ResponseSections, 1, "Expected one response section")
	firstResponseSection := mongoRequestValue.ResponseSections[0]
	assert.Equal(t, sectionTypeBody, firstResponseSection.Type, "Expected first section type to be sectionTypeBody")
	assert.Equal(t, defaultResponseData, firstResponseSection.Body, "Expected first section body to match request data")
}

// getMongoInfo

func TestGetMongoInfoFindRequest(t *testing.T) {
	mongoRequest := MongoRequestValue{
		RequestSections: []mongoSection{
			{
				Type: sectionTypeBody,
				Body: bson.D{bson.E{Key: commFind, Value: "my_collection"}, bson.E{Key: "$db", Value: "my_db"}},
			},
		},
		ResponseSections: []mongoSection{
			{
				Type: sectionTypeBody,
				Body: bson.D{bson.E{Key: "ok", Value: float64(1)}},
			},
		},
	}
	res, err := getMongoInfo(&mongoRequest)
	require.NoError(t, err, "Expected no error when mongodb failed")
	assert.Equal(t, "my_db", res.DB, "Expected DB to be 'my_db'")
	assert.Equal(t, "my_collection", res.Collection, "Expected Collection to be 'my_collection'")
	assert.Equal(t, commFind, res.OpName, "Expected Operation to be 'find'")
	assert.True(t, res.Success, "Expected Response to be 'ok'")
	assert.Empty(t, res.Error, "Expected Error to be empty in successful request")
	assert.Empty(t, res.ErrorCode, "Expected ErrorCode to be empty in successful request")
	assert.Empty(t, res.ErrorCodeName, "Expected ErrorCodeName to be empty in successful request")
}

func TestGetMongoInfoErrorRequest(t *testing.T) {
	mongoRequest := MongoRequestValue{
		RequestSections: []mongoSection{
			{
				Type: sectionTypeBody,
				Body: bson.D{bson.E{Key: commFind, Value: "my_collection"}, bson.E{Key: "$db", Value: "my_db"}},
			},
		},
		ResponseSections: []mongoSection{
			{
				Type: sectionTypeBody,
				Body: bson.D{bson.E{Key: "ok", Value: float64(0)}, bson.E{Key: "errmsg", Value: "some error"}, bson.E{Key: "code", Value: 12345}, bson.E{Key: "codeName", Value: "SomeError"}},
			},
		},
	}
	res, err := getMongoInfo(&mongoRequest)
	require.NoError(t, err, "Expected no error when mongodb failed")
	assert.Equal(t, "my_db", res.DB, "Expected DB to be 'my_db'")
	assert.Equal(t, "my_collection", res.Collection, "Expected Collection to be 'my_collection'")
	assert.Equal(t, commFind, res.OpName, "Expected Operation to be 'find'")
	assert.False(t, res.Success, "Expected Response to not be 'ok'")
	assert.Equal(t, "some error", res.Error, "Expected Error to be 'some error'")
	assert.Equal(t, 12345, res.ErrorCode, "Expected ErrorCode to be 12345")
	assert.Equal(t, "SomeError", res.ErrorCodeName, "Expected ErrorCodeName to be 'SomeError'")
}

func TestGetMongoInfoNoResponseSectionShouldBeSuccess(t *testing.T) {
	mongoRequest := MongoRequestValue{
		RequestSections: []mongoSection{
			{
				Type: sectionTypeBody,
				Body: bson.D{bson.E{Key: commFind, Value: "my_collection"}, bson.E{Key: "$db", Value: "my_db"}},
			},
		},
		ResponseSections: []mongoSection{},
	}
	res, err := getMongoInfo(&mongoRequest)
	require.NoError(t, err, "Expected no error when mongodb failed")
	assert.Equal(t, "my_db", res.DB, "Expected DB to be 'my_db'")
	assert.Equal(t, "my_collection", res.Collection, "Expected Collection to be 'my_collection'")
	assert.Equal(t, commFind, res.OpName, "Expected Operation to be 'find'")
	assert.True(t, res.Success, "Expected Response to be 'ok'")
	assert.Empty(t, res.Error, "Expected Error to be empty in successful request")
	assert.Empty(t, res.ErrorCode, "Expected ErrorCode to be empty in successful request")
	assert.Empty(t, res.ErrorCodeName, "Expected ErrorCodeName to be empty in successful request")
}

func TestGetMongoInfoFailWhenHealthCommand(t *testing.T) {
	mongoRequest := MongoRequestValue{
		RequestSections: []mongoSection{
			{
				Type: sectionTypeBody,
				Body: bson.D{bson.E{Key: commHello}},
			},
		},
		ResponseSections: []mongoSection{
			{
				Type: sectionTypeBody,
				Body: bson.D{bson.E{Key: "ok", Value: float64(1)}},
			},
		},
	}
	_, err := getMongoInfo(&mongoRequest)
	assert.Error(t, err, "Expected error when processing health command")
}

func TestGetMongoInfoWithUnknownCommand(t *testing.T) {
	mongoRequest := MongoRequestValue{
		RequestSections: []mongoSection{
			{
				Type: sectionTypeBody,
				Body: bson.D{bson.E{Key: "createUser", Value: "my_collection"}},
			},
		},
		ResponseSections: []mongoSection{},
	}
	res, err := getMongoInfo(&mongoRequest)
	require.NoError(t, err, "Expected no error when mongodb failed")
	assert.Empty(t, res.DB, "Expected DB to be empty for unknown command")
	assert.Empty(t, res.Collection, "Expected Collection to be empty for unknown command")
	assert.Equal(t, "createUser", res.OpName, "Expected Operation to be 'find'")
	assert.True(t, res.Success, "Expected Response to be 'ok'")
	assert.Empty(t, res.Error, "Expected Error to be empty in successful request")
	assert.Empty(t, res.ErrorCode, "Expected ErrorCode to be empty in successful request")
	assert.Empty(t, res.ErrorCodeName, "Expected ErrorCodeName to be empty in successful request")
}

func TestGetMongoInfoOperationUnknownForEmptyRequestSection(t *testing.T) {
	mongoRequest := MongoRequestValue{
		RequestSections: []mongoSection{
			{
				Type: sectionTypeBody,
				Body: bson.D{},
			},
		},
		ResponseSections: []mongoSection{},
	}
	res, err := getMongoInfo(&mongoRequest)
	require.NoError(t, err, "Expected no error when mongodb failed")
	assert.Empty(t, res.DB, "Expected DB to be empty for unknown command")
	assert.Empty(t, res.Collection, "Expected Collection to be empty for unknown command")
	assert.Equal(t, "*", res.OpName, "Expected Operation to be 'find'")
	assert.True(t, res.Success, "Expected Response to be 'ok'")
	assert.Empty(t, res.Error, "Expected Error to be empty in successful request")
	assert.Empty(t, res.ErrorCode, "Expected ErrorCode to be empty in successful request")
	assert.Empty(t, res.ErrorCodeName, "Expected ErrorCodeName to be empty in successful request")
}

func TestProcessMongoEventRealWorldInsertPayload(t *testing.T) {
	defer requests.Purge()

	// Real-world MongoDB insert payload that was failing to parse correctly
	// This payload contains a truncated BSON document which was causing parsing issues
	payload := []byte{
		162, 1, 0, 0, 43, 69, 1, 0, 0, 0, 0, 0, 221, 7, 0, 0, 0, 0, 0, 0, 0, 141, 1, 0, 0, 2, 105, 110, 115, 101, 114, 116, 0, 6, 0, 0, 0, 117, 115, 101, 114, 115, 0, 4, 100, 111, 99, 117, 109, 101, 110, 116, 115, 0, 147, 0, 0, 0, 3, 48, 0, 139, 0, 0, 0, 2, 110, 97, 109, 101, 0, 13, 0, 0, 0, 65, 97, 109, 105, 114, 32, 75, 104, 97, 110, 32, 49, 0, 2, 101, 109, 97, 105, 108, 0, 27, 0, 0, 0, 97, 97, 109, 105, 114, 107, 104, 97, 110, 49, 64, 101, 120, 97, 109, 115, 102, 115, 102, 112, 108, 101, 46, 99, 111, 109, 0, 16, 97, 103, 101, 0, 55, 0, 0, 0, 7, 95, 105, 100, 0, 104, 117, 12, 55, 255, 115, 113, 127, 76, 26, 180, 31, 9, 99, 114, 101, 97, 116, 101, 100, 65, 116, 0, 77, 183, 55, 9, 152, 1, 0, 0, 9, 117, 112, 100, 97, 116, 101, 100, 65, 116, 0, 77, 183, 55, 9, 152, 1, 0, 0, 16, 95, 95, 118, 0, 0, 0, 0, 0, 0, 0, 8, 111, 114, 100, 101, 114, 101, 100, 0, 1, 3, 119, 114, 105, 116, 101, 67, 111, 110, 99, 101, 114, 110, 0, 21, 0, 0, 0, 2, 119, 0, 9, 0, 0, 0, 109, 97, 106, 111, 114, 105, 116, 121, 0, 0, 3, 108, 115, 105, 100, 0, 30, 0, 0, 0,
	}

	connInfo := getConnInfo()

	// Process the payload
	mongoRequestValue, moreToCome, err := ProcessMongoEvent(payload, StartTime, EndTime, connInfo, requests)
	require.NoError(t, err, "Expected no error for real-world MongoDB insert payload")

	// The function should return nil for requests (only returns value for responses)
	// But it should store the request in the cache and return moreToCome=true
	assert.Nil(t, mongoRequestValue, "Expected nil MongoRequestValue for request")
	assert.True(t, moreToCome, "Expected more to come for request")

	// Check if there's a request stored in the cache
	header, _ := parseMongoHeader(payload)
	key := makeRequestKey(false, header, connInfo)
	storedRequest, found := requests.Get(key)
	assert.True(t, found, "Expected request to be stored in cache")

	// Verify the stored request has the correct structure
	require.NotNil(t, storedRequest, "Expected stored request to not be nil")
	assert.Len(t, storedRequest.RequestSections, 1, "Expected one request section")
	assert.Len(t, storedRequest.ResponseSections, 0, "Expected no response sections")

	// Verify we can extract mongo info from the stored request
	info, infoErr := getMongoInfo(storedRequest)
	require.NoError(t, infoErr, "Expected no error getting mongo info")

	// Verify the operation name and collection are correctly parsed
	assert.Equal(t, "insert", info.OpName, "Expected operation name to be 'insert'")
	assert.Equal(t, "users", info.Collection, "Expected collection to be 'users'")
	assert.True(t, info.Success, "Expected operation to be successful")

	// Note: DB field is empty because it's not in the truncated payload
	// This is expected behavior for partial/truncated data
}

func TestProcessMongoEventRealWorldResponsePayload(t *testing.T) {
	defer requests.Purge()

	// Real-world MongoDB response payload that was failing with "no 'ok' field found in MongoDB response"
	// This payload contains a response with double-type 'ok' field that was being skipped by the parser
	responsePayload := []byte{
		230, 0, 0, 0, 156, 161, 55, 7, 161, 75, 1, 0, 221, 7, 0, 0, 0, 0, 0, 0, 0, 209, 0, 0, 0, 16, 110, 0, 1, 0, 0, 0, 7, 101, 108, 101, 99, 116, 105, 111, 110, 73, 100, 0, 127, 255, 255, 255, 0, 0, 0, 0, 0, 0, 1, 82, 3, 111, 112, 84, 105, 109, 101, 0, 28, 0, 0, 0, 17, 116, 115, 0, 7, 0, 0, 0, 161, 33, 117, 104, 18, 116, 0, 82, 1, 0, 0, 0, 0, 0, 0, 0, 1, 111, 107, 0, 0, 0, 0, 0, 0, 0, 240, 63, 3, 36, 99, 108, 117, 115, 116, 101, 114, 84, 105, 109, 101, 0, 88, 0, 0, 0, 17, 99, 108, 117, 115, 116,
	}

	connInfo := getConnInfo()

	// Parse header to understand the structure
	header, headerErr := parseMongoHeader(responsePayload)
	require.NoError(t, headerErr, "Expected header parsing to succeed")

	// Since this is a response (ResponseTo != 0), we need to create a request first
	requestHeader := &msgHeader{
		MessageLength: MessageLength,
		RequestID:     header.ResponseTo, // Use the ResponseTo as the original request ID
		ResponseTo:    0,
		OpCode:        opMsg,
	}
	requestData := bson.D{bson.E{Key: "find", Value: "test_collection"}}
	requestPayload := getRequestPayload(requestHeader, 0, sectionTypeBody, &requestData)

	// Process the request first
	_, moreToCome, err := ProcessMongoEvent(requestPayload, StartTime, EndTime, connInfo, requests)
	require.NoError(t, err, "Expected no error for request")
	assert.True(t, moreToCome, "Expected more to come for request")

	// Process the response
	mongoRequestValue, moreToCome, err := ProcessMongoEvent(responsePayload, StartTime, EndTime, connInfo, requests)
	require.NoError(t, err, "Expected no error for response")
	assert.False(t, moreToCome, "Expected no more to come for response")
	require.NotNil(t, mongoRequestValue, "Expected response to be returned")

	// Verify we can extract mongo info from the response
	info, infoErr := getMongoInfo(mongoRequestValue)
	require.NoError(t, infoErr, "Expected no error getting mongo info")

	// Verify the response is correctly parsed
	assert.Equal(t, "find", info.OpName, "Expected operation name to be 'find'")
	assert.Equal(t, "test_collection", info.Collection, "Expected collection to be 'test_collection'")
	assert.True(t, info.Success, "Expected operation to be successful")
	assert.Empty(t, info.Error, "Expected no error message")

	// Verify the response sections contain the 'ok' field
	require.Len(t, mongoRequestValue.ResponseSections, 1, "Expected one response section")
	responseSection := mongoRequestValue.ResponseSections[0]

	// Check that the 'ok' field is found and has the correct value
	okValue, okFound := findDoubleInBson(responseSection.Body, "ok")
	assert.True(t, okFound, "Expected 'ok' field to be found in response")
	assert.Equal(t, 1.0, okValue, "Expected 'ok' field to have value 1.0")
}
