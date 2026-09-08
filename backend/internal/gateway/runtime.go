package gateway

import "context"

// Core invokes the SDK drain phase before retiring a process. Stop background
// admission immediately while existing user requests finish on this generation.
func (g *AnthropicGateway) BeginDrain(context.Context) error {
	if g.sidecar != nil {
		g.sidecar.beginDrain()
	}
	return nil
}
