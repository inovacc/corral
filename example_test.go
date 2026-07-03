package agents_test

import (
	"fmt"

	"github.com/inovacc/agents"
)

// Example_registerAgent shows a consumer supplying its own agent roster. The
// runtime ships no built-in agents: the host application registers the agents it
// needs with Register (a lazy factory) and reads them back, sorted by name, with
// All.
func Example_registerAgent() {
	agents.Register(func() agents.Agent {
		return agents.Agent{
			Name:        "demo",
			Kind:        agents.KindControl,
			Description: "example agent supplied by the host application",
			Tools:       []string{"agents"},
			System:      "You are a demo agent.",
		}
	})

	for _, a := range agents.All() {
		fmt.Printf("%s (%s): %s\n", a.Name, a.Kind, a.Description)
	}
	// Output:
	// demo (control): example agent supplied by the host application
}
