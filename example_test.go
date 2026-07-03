package corral_test

import (
	"fmt"

	"github.com/inovacc/corral"
)

// Example_registerAgent shows a consumer supplying its own agent roster. The
// runtime ships no built-in agents: the host application registers the agents it
// needs with Register (a lazy factory) and reads them back, sorted by name, with
// All.
func Example_registerAgent() {
	corral.Register(func() corral.Agent {
		return corral.Agent{
			Name:        "demo",
			Kind:        corral.KindControl,
			Description: "example agent supplied by the host application",
			Tools:       []string{"agents"},
			System:      "You are a demo agent.",
		}
	})

	for _, a := range corral.All() {
		fmt.Printf("%s (%s): %s\n", a.Name, a.Kind, a.Description)
	}
	// Output:
	// demo (control): example agent supplied by the host application
}
