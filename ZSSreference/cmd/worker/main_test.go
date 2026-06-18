package main

import (
	"strings"
	"testing"

	"github.com/SATA260/SimulSpeak1/internal/config"
	"github.com/SATA260/SimulSpeak1/internal/model"
	"github.com/SATA260/SimulSpeak1/internal/worker/vocabulary"
)

func TestWorkerRegistryNodeUsesEnvAndConsumerCapacity(t *testing.T) {
	t.Setenv("SIMULSPEAK_WORKER_ID", "vocab")
	t.Setenv("SIMULSPEAK_WORKER_NODE_ID", "worker/node:1")

	consumer := vocabulary.NewConsumer(nil, nil, vocabulary.Options{
		WorkerID:    "vocab",
		Concurrency: 3,
	}, nil)
	node := workerRegistryNode(&config.AppConfig{
		Service: config.ServiceConfig{Name: "worker"},
		Node: config.NodeConfig{
			Zone:   "az-a",
			Weight: 2,
		},
	}, consumer)

	if node.ID != "worker-node-1" || node.Endpoint != "worker://worker-node-1" {
		t.Fatalf("unexpected node identity: %#v", node)
	}
	if node.Type != model.NodeTypeWorker || node.Status != model.NodeStatusUp {
		t.Fatalf("unexpected node type/status: %#v", node)
	}
	if node.MaxCalls != 3 || node.CurrentCalls != 0 || node.Zone != "az-a" || node.Weight != 2 {
		t.Fatalf("unexpected node load fields: %#v", node)
	}
	if strings.Join(node.Capabilities, ",") != "vocabulary" || node.Labels["workerId"] != "vocab" {
		t.Fatalf("unexpected node metadata: %#v", node)
	}
}
