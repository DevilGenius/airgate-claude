package main

import (
	"github.com/DevilGenius/airgate-claude/backend/internal/gateway"
	sdkgrpc "github.com/DevilGenius/airgate-sdk/runtimego/grpc"
)

func main() {
	sdkgrpc.Serve(&gateway.AnthropicGateway{})
}
