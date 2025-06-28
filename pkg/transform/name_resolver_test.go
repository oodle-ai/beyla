package transform

import (
	"testing"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/stretchr/testify/assert"

	attr "github.com/grafana/beyla/v2/pkg/export/attributes/names"
	kube2 "github.com/grafana/beyla/v2/pkg/internal/kube"
	"github.com/grafana/beyla/v2/pkg/internal/request"
	"github.com/grafana/beyla/v2/pkg/internal/svc"
	"github.com/grafana/beyla/v2/pkg/kubecache/informer"
)

func TestSuffixPrefix(t *testing.T) {
	suffixTests := []struct {
		name     string
		str      string
		suffix   string
		expected string
	}{
		{
			name:     "match case insensitive suffix",
			str:      "superDuper",
			suffix:   "DUPER",
			expected: "super",
		},
		{
			name:     "match partial suffix",
			str:      "superDuper",
			suffix:   "ER",
			expected: "superDup",
		},
		{
			name:     "no match",
			str:      "superDuper",
			suffix:   "Not matching",
			expected: "superDuper",
		},
		{
			name:     "suffix longer than string",
			str:      "superDuper",
			suffix:   "SuperDuperDuper",
			expected: "superDuper",
		},
		{
			name:     "exact match",
			str:      "superDuper",
			suffix:   "SuperDuper",
			expected: "",
		},
		{
			name:     "empty suffix",
			str:      "superDuper",
			suffix:   "",
			expected: "superDuper",
		},
	}

	for _, tt := range suffixTests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := trimSuffixIgnoreCase(tt.str, tt.suffix)
			assert.Equal(t, tt.expected, result)
		})
	}

	prefixTests := []struct {
		name     string
		str      string
		prefix   string
		expected string
	}{
		{
			name:     "match case insensitive prefix",
			str:      "Dupersuper",
			prefix:   "DUPER",
			expected: "super",
		},
		{
			name:     "match partial prefix",
			str:      "Ersuper",
			prefix:   "ER",
			expected: "super",
		},
		{
			name:     "no match",
			str:      "superDuper",
			prefix:   "Not matching",
			expected: "superDuper",
		},
		{
			name:     "prefix longer than string",
			str:      "superDuper",
			prefix:   "SuperDuperDuper",
			expected: "superDuper",
		},
		{
			name:     "exact match",
			str:      "superDuper",
			prefix:   "SuperDuper",
			expected: "",
		},
		{
			name:     "empty prefix",
			str:      "superDuper",
			prefix:   "",
			expected: "superDuper",
		},
	}

	for _, tt := range prefixTests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := trimPrefixIgnoreCase(tt.str, tt.prefix)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolvePodsFromK8s(t *testing.T) {
	inf := &fakeInformer{}
	db := kube2.NewStore(inf, kube2.ResourceLabels{})
	pod1 := &informer.ObjectMeta{Name: "pod1", Kind: "Pod", Ips: []string{"10.0.0.1", "10.1.0.1"}}
	pod2 := &informer.ObjectMeta{Name: "pod2", Namespace: "something", Kind: "Pod", Ips: []string{"10.0.0.2", "10.1.0.2"}}
	pod3 := &informer.ObjectMeta{Name: "pod3", Kind: "Pod", Ips: []string{"10.0.0.3", "10.1.0.3"}}
	inf.Notify(&informer.Event{Type: informer.EventType_CREATED, Resource: pod1})
	inf.Notify(&informer.Event{Type: informer.EventType_CREATED, Resource: pod2})
	inf.Notify(&informer.Event{Type: informer.EventType_CREATED, Resource: pod3})

	assert.Equal(t, pod1, db.ObjectMetaByIP("10.0.0.1").Meta)
	assert.Equal(t, pod1, db.ObjectMetaByIP("10.1.0.1").Meta)
	assert.Equal(t, pod2, db.ObjectMetaByIP("10.0.0.2").Meta)
	assert.Equal(t, pod2, db.ObjectMetaByIP("10.1.0.2").Meta)
	assert.Equal(t, pod3, db.ObjectMetaByIP("10.1.0.3").Meta)

	inf.Notify(&informer.Event{Type: informer.EventType_DELETED, Resource: pod3})
	assert.Nil(t, db.ObjectMetaByIP("10.1.0.3"))

	nr := NameResolver{
		db:      db,
		cache:   expirable.NewLRU[string, string](10, nil, 5*time.Hour),
		sources: resolverSources([]string{"dns", "k8s"}),
	}

	name, namespace := nr.resolveFromK8s("10.0.0.1")
	assert.Equal(t, "pod1", name)
	assert.Equal(t, "", namespace)

	name, namespace = nr.resolveFromK8s("10.0.0.2")
	assert.Equal(t, "pod2", name)
	assert.Equal(t, "something", namespace)

	name, namespace = nr.resolveFromK8s("10.0.0.3")
	assert.Equal(t, "", name)
	assert.Equal(t, "", namespace)

	clientSpan := request.Span{
		Type: request.EventTypeHTTPClient,
		Peer: "10.0.0.1",
		Host: "10.0.0.2",
		Service: svc.Attrs{UID: svc.UID{
			Name:      "pod1",
			Namespace: "",
		}},
	}

	serverSpan := request.Span{
		Type: request.EventTypeHTTP,
		Peer: "10.0.0.1",
		Host: "10.0.0.2",
		Service: svc.Attrs{UID: svc.UID{
			Name:      "pod2",
			Namespace: "something",
		}},
	}

	nr.resolveNames(&clientSpan)

	assert.Equal(t, "pod1", clientSpan.PeerName)
	assert.Equal(t, "", clientSpan.Service.UID.Namespace)
	assert.Equal(t, "pod2", clientSpan.HostName)
	assert.Equal(t, "something", clientSpan.OtherNamespace)

	nr.resolveNames(&serverSpan)

	assert.Equal(t, "pod1", serverSpan.PeerName)
	assert.Equal(t, "", serverSpan.OtherNamespace)
	assert.Equal(t, "pod2", serverSpan.HostName)
	assert.Equal(t, "something", serverSpan.Service.UID.Namespace)
}

func TestResolveServiceFromK8s(t *testing.T) {
	inf := &fakeInformer{}
	db := kube2.NewStore(inf, kube2.ResourceLabels{})
	pod1 := &informer.ObjectMeta{Name: "pod1", Kind: "Service", Ips: []string{"10.0.0.1", "10.1.0.1"}}
	pod2 := &informer.ObjectMeta{Name: "pod2", Namespace: "something", Kind: "Service", Ips: []string{"10.0.0.2", "10.1.0.2"}}
	pod3 := &informer.ObjectMeta{Name: "pod3", Kind: "Service", Ips: []string{"10.0.0.3", "10.1.0.3"}}
	inf.Notify(&informer.Event{Type: informer.EventType_CREATED, Resource: pod1})
	inf.Notify(&informer.Event{Type: informer.EventType_CREATED, Resource: pod2})
	inf.Notify(&informer.Event{Type: informer.EventType_CREATED, Resource: pod3})

	assert.Equal(t, pod1, db.ObjectMetaByIP("10.0.0.1").Meta)
	assert.Equal(t, pod1, db.ObjectMetaByIP("10.1.0.1").Meta)
	assert.Equal(t, pod2, db.ObjectMetaByIP("10.0.0.2").Meta)
	assert.Equal(t, pod2, db.ObjectMetaByIP("10.1.0.2").Meta)
	assert.Equal(t, pod3, db.ObjectMetaByIP("10.1.0.3").Meta)
	inf.Notify(&informer.Event{Type: informer.EventType_DELETED, Resource: pod3})
	assert.Nil(t, db.ObjectMetaByIP("10.1.0.3"))

	nr := NameResolver{
		db:      db,
		cache:   expirable.NewLRU[string, string](10, nil, 5*time.Hour),
		sources: resolverSources([]string{"dns", "k8s"}),
	}

	name, namespace := nr.resolveFromK8s("10.0.0.1")
	assert.Equal(t, "pod1", name)
	assert.Equal(t, "", namespace)

	name, namespace = nr.resolveFromK8s("10.0.0.2")
	assert.Equal(t, "pod2", name)
	assert.Equal(t, "something", namespace)

	name, namespace = nr.resolveFromK8s("10.0.0.3")
	assert.Equal(t, "", name)
	assert.Equal(t, "", namespace)

	clientSpan := request.Span{
		Type: request.EventTypeHTTPClient,
		Peer: "10.0.0.1",
		Host: "10.0.0.2",
		Service: svc.Attrs{UID: svc.UID{
			Name:      "pod1",
			Namespace: "",
		}},
	}

	serverSpan := request.Span{
		Type: request.EventTypeHTTP,
		Peer: "10.0.0.1",
		Host: "10.0.0.2",
		Service: svc.Attrs{UID: svc.UID{
			Name:      "pod2",
			Namespace: "something",
		}},
	}

	nr.resolveNames(&clientSpan)

	assert.Equal(t, "pod1", clientSpan.PeerName)
	assert.Equal(t, "", clientSpan.Service.UID.Namespace)
	assert.Equal(t, "pod2", clientSpan.HostName)
	assert.Equal(t, "something", clientSpan.OtherNamespace)

	nr.resolveNames(&serverSpan)

	assert.Equal(t, "pod1", serverSpan.PeerName)
	assert.Equal(t, "", serverSpan.OtherNamespace)
	assert.Equal(t, "pod2", serverSpan.HostName)
	assert.Equal(t, "something", serverSpan.Service.UID.Namespace)
}

func TestCleanName(t *testing.T) {
	s := svc.Attrs{
		UID: svc.UID{
			Name:      "service",
			Namespace: "special.namespace",
		},
		Metadata: map[attr.Name]string{
			attr.K8sNamespaceName: "k8snamespace",
		},
	}

	nr := NameResolver{}

	tests := []struct {
		name     string
		ip       string
		hostname string
		wantName string
		wantNS   string
	}{
		{
			name:     "ip-based hostname",
			ip:       "127.0.0.1",
			hostname: "127-0-0-1.service",
			wantName: "service",
			wantNS:   "special.namespace",
		},
		{
			name:     "hostname with number prefix",
			ip:       "127.0.0.1",
			hostname: "1.service",
			wantName: "1.service",
			wantNS:   "",
		},
		{
			name:     "hostname with trailing dot",
			ip:       "127.0.0.1",
			hostname: "service.",
			wantName: "service",
			wantNS:   "",
		},
		{
			name:     "hostname with cluster suffix",
			ip:       "127.0.0.1",
			hostname: "service.svc.cluster.local.",
			wantName: "service",
			wantNS:   "special.namespace",
		},
		{
			name:     "hostname with namespace and cluster suffix",
			ip:       "127.0.0.1",
			hostname: "service.special.namespace.svc.cluster.local.",
			wantName: "service",
			wantNS:   "special.namespace",
		},
		{
			name:     "hostname with k8s namespace and cluster suffix",
			ip:       "127.0.0.1",
			hostname: "service.k8snamespace.svc.cluster.local.",
			wantName: "service",
			wantNS:   "k8snamespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotNS := nr.cleanName(&s, tt.ip, tt.hostname)
			assert.Equal(t, tt.wantName, gotName)
			assert.Equal(t, tt.wantNS, gotNS)
		})
	}
}

func TestResolveNodesFromK8s(t *testing.T) {
	inf := &fakeInformer{}
	db := kube2.NewStore(inf, kube2.ResourceLabels{})
	node1 := &informer.ObjectMeta{Name: "node1", Kind: "Node", Ips: []string{"10.0.0.1", "10.1.0.1"}}
	node2 := &informer.ObjectMeta{Name: "node2", Namespace: "something", Kind: "Node", Ips: []string{"10.0.0.2", "10.1.0.2"}}
	node3 := &informer.ObjectMeta{Name: "node3", Kind: "Node", Ips: []string{"10.0.0.3", "10.1.0.3"}}
	inf.Notify(&informer.Event{Type: informer.EventType_CREATED, Resource: node1})
	inf.Notify(&informer.Event{Type: informer.EventType_CREATED, Resource: node2})
	inf.Notify(&informer.Event{Type: informer.EventType_CREATED, Resource: node3})

	assert.Equal(t, node1, db.ObjectMetaByIP("10.0.0.1").Meta)
	assert.Equal(t, node1, db.ObjectMetaByIP("10.1.0.1").Meta)
	assert.Equal(t, node2, db.ObjectMetaByIP("10.0.0.2").Meta)
	assert.Equal(t, node2, db.ObjectMetaByIP("10.1.0.2").Meta)
	assert.Equal(t, node3, db.ObjectMetaByIP("10.1.0.3").Meta)
	inf.Notify(&informer.Event{Type: informer.EventType_DELETED, Resource: node3})
	assert.Nil(t, db.ObjectMetaByIP("10.1.0.3"))

	nr := NameResolver{
		db:      db,
		cache:   expirable.NewLRU[string, string](10, nil, 5*time.Hour),
		sources: resolverSources([]string{"dns", "k8s"}),
	}

	name, namespace := nr.resolveFromK8s("10.0.0.1")
	assert.Equal(t, "node1", name)
	assert.Equal(t, "", namespace)

	name, namespace = nr.resolveFromK8s("10.0.0.2")
	assert.Equal(t, "node2", name)
	assert.Equal(t, "something", namespace)

	name, namespace = nr.resolveFromK8s("10.0.0.3")
	assert.Equal(t, "", name)
	assert.Equal(t, "", namespace)

	clientSpan := request.Span{
		Type: request.EventTypeHTTPClient,
		Peer: "10.0.0.1",
		Host: "10.0.0.2",
		Service: svc.Attrs{UID: svc.UID{
			Name:      "node1",
			Namespace: "",
		}},
	}

	serverSpan := request.Span{
		Type: request.EventTypeHTTP,
		Peer: "10.0.0.1",
		Host: "10.0.0.2",
		Service: svc.Attrs{UID: svc.UID{
			Name:      "node2",
			Namespace: "something",
		}},
	}

	nr.resolveNames(&clientSpan)

	assert.Equal(t, "node1", clientSpan.PeerName)
	assert.Equal(t, "", clientSpan.Service.UID.Namespace)
	assert.Equal(t, "node2", clientSpan.HostName)
	assert.Equal(t, "something", clientSpan.OtherNamespace)

	nr.resolveNames(&serverSpan)

	assert.Equal(t, "node1", serverSpan.PeerName)
	assert.Equal(t, "", serverSpan.OtherNamespace)
	assert.Equal(t, "node2", serverSpan.HostName)
	assert.Equal(t, "something", serverSpan.Service.UID.Namespace)
}
