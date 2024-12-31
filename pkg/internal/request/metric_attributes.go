package request

import (
	"net"
	"strings"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.19.0"

	attr "github.com/grafana/beyla/pkg/export/attributes/names"
)

func HTTPRequestMethod(val string) attribute.KeyValue {
	return attribute.Key(attr.HTTPRequestMethod).String(val)
}

func HTTPResponseStatusCode(val int) attribute.KeyValue {
	return attribute.Key(attr.HTTPResponseStatusCode).Int(val)
}

func HTTPUrlPath(val string) attribute.KeyValue {
	return attribute.Key(attr.HTTPUrlPath).String(val)
}

func HTTPUrlFull(val string) attribute.KeyValue {
	return attribute.Key(attr.HTTPUrlFull).String(val)
}

func ClientAddr(val string) attribute.KeyValue {
	return attribute.Key(attr.ClientAddr).String(val)
}

func ServerAddr(val string) attribute.KeyValue {
	return attribute.Key(attr.ServerAddr).String(val)
}

func ServerPort(val int) attribute.KeyValue {
	return attribute.Key(attr.ServerPort).Int(val)
}

func HTTPRequestBodySize(val int) attribute.KeyValue {
	return attribute.Key(attr.HTTPRequestBodySize).Int(val)
}

func SpanKindMetric(val string) attribute.KeyValue {
	return attribute.Key(attr.SpanKind).String(val)
}

func SpanNameMetric(val string) attribute.KeyValue {
	return attribute.Key(attr.SpanName).String(val)
}

func SourceMetric(val string) attribute.KeyValue {
	return attribute.Key(attr.Source).String(val)
}

func ServiceMetric(val string) attribute.KeyValue {
	return attribute.Key(attr.Service).String(val)
}

func StatusCodeMetric(val int) attribute.KeyValue {
	return attribute.Key(attr.StatusCode).Int(val)
}

func ClientMetric(val string) attribute.KeyValue {
	return attribute.Key(attr.Client).String(val)
}

func ClientNamespaceMetric(val string) attribute.KeyValue {
	return attribute.Key(attr.ClientNamespace).String(val)
}

func ServerMetric(val string) attribute.KeyValue {
	return attribute.Key(attr.Server).String(val)
}

func ServerNamespaceMetric(val string) attribute.KeyValue {
	return attribute.Key(attr.ServerNamespace).String(val)
}

func ConnectionTypeMetric(val string) attribute.KeyValue {
	return attribute.Key(attr.ConnectionType).String(val)
}

func DBQueryText(val string) attribute.KeyValue {
	return attribute.Key(attr.DBQueryText).String(val)
}

func DBCollectionName(val string) attribute.KeyValue {
	return attribute.Key(attr.DBCollectionName).String(val)
}

func DBOperationName(val string) attribute.KeyValue {
	return attribute.Key(attr.DBOperation).String(val)
}

func DBSystem(val string) attribute.KeyValue {
	return attribute.Key(semconv.DBSystemKey).String(val)
}

func ErrorType(val string) attribute.KeyValue {
	return attribute.Key(attr.ErrorType).String(val)
}

func MessagingOperationType(val string) attribute.KeyValue {
	return attribute.Key(attr.MessagingOpType).String(val)
}

func SpanHost(span *Span) string {
	return SpanHostResolvingDNS(span, nil)
}

func SpanHostResolvingDNS(span *Span, dnsCache *expirable.LRU[string, string]) string {
	if span.HostName != "" {
		return span.HostName
	}

	if addr := getAddrFromIP(span.Host, dnsCache); len(addr) > 0 {
		return addr
	}

	return span.Host
}

func getAddrFromIP(ip string, dnsCache *expirable.LRU[string, string]) string {
	if dnsCache == nil {
		return ""
	}

	if addr, ok := dnsCache.Get(ip); ok {
		return addr
	}

	if net.ParseIP(ip) != nil {
		domainNames, err := net.LookupAddr(ip)
		if err == nil && len(domainNames) > 0 {
			dnsCache.Add(ip, domainNames[0])
			return strings.TrimSuffix(domainNames[0], ".")
		}
	}

	return ip
}

func SpanPeerResolvingDNS(span *Span, dnsCache *expirable.LRU[string, string]) string {
	if span.PeerName != "" {
		return span.PeerName
	}

	if addr := getAddrFromIP(span.Peer, dnsCache); len(addr) > 0 {
		return addr
	}

	return span.Peer
}

func SpanPeer(span *Span) string {
	return SpanPeerResolvingDNS(span, nil)
}

func HostAsServer(span *Span) string {
	if span.OtherNamespace != "" && span.OtherNamespace != span.Service.UID.Namespace && span.HostName != "" {
		if span.IsClientSpan() {
			return SpanHost(span) + "." + span.OtherNamespace
		}
	}

	return SpanHost(span)
}

func PeerAsClient(span *Span) string {
	if span.OtherNamespace != "" && span.OtherNamespace != span.Service.UID.Namespace && span.PeerName != "" {
		if !span.IsClientSpan() {
			return SpanPeer(span) + "." + span.OtherNamespace
		}
	}

	return SpanPeer(span)
}
