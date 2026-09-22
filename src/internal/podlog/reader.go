// Package podlog reads the tail of a container's output. The controller-runtime client cannot:
// logs are a subresource served by the kubelet, not an object in the cache, so this is the one
// place the operator holds a clientset.
//
// Used to observe what the Neo4j image entrypoint reports about plugin installation, which it
// prints and then continues past (BDR-004, ADR-009).
package podlog

import (
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Reader returns the opening bytes of one container's log.
type Reader interface {
	// Head returns at most limit bytes from the start of the named container's output. The start,
	// not the tail: the entrypoint runs before the server does, so what it reports about plugins
	// is the first thing in the log and would be long out of a tail window on a container that
	// has been up for days. A container that has restarted serves its current instance; callers
	// wanting the instance that died ask for previous.
	Head(ctx context.Context, namespace, pod, container string, limit int64, previous bool) (string, error)
}

// ClientsetReader reads logs through the Kubernetes API.
type ClientsetReader struct {
	clientset kubernetes.Interface
}

// New builds a Reader from the manager's REST config.
func New(cfg *rest.Config) (*ClientsetReader, error) {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("plugin log reader: %w", err)
	}
	return &ClientsetReader{clientset: cs}, nil
}

// NewFromInterface builds a Reader over an existing clientset, which is how tests inject a fake.
func NewFromInterface(cs kubernetes.Interface) *ClientsetReader {
	return &ClientsetReader{clientset: cs}
}

func (r *ClientsetReader) Head(ctx context.Context, namespace, pod, container string,
	limit int64, previous bool) (string, error) {
	if r == nil || r.clientset == nil {
		return "", nil
	}
	// LimitBytes with no TailLines streams from the beginning and stops, which bounds what crosses
	// the wire without moving the window away from the entrypoint's output.
	opts := &corev1.PodLogOptions{
		Container:  container,
		LimitBytes: &limit,
		Previous:   previous,
	}
	stream, err := r.clientset.CoreV1().Pods(namespace).GetLogs(pod, opts).Stream(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = stream.Close() }()
	body, err := io.ReadAll(stream)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
