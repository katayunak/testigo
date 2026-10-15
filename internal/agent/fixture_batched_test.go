package agent

import "github.com/katayunak/gofacts/fact"

func batchedFixture() *fact.Flow {
	f := fixtureFlow()
	process := f.Seams[0].In
	f.Seams = append(f.Seams,
		fact.Seam{In: process, Kind: fact.SeamHTTP,
			Target: "(example.com/paysvc/psp.Gateway).Capture", Line: 71,
			Injectable: true, Iface: "example.com/paysvc/psp.Gateway"},
		fact.Seam{In: process, Kind: fact.SeamQueue,
			Target: "(example.com/paysvc/bus.Publisher).Publish", Line: 80,
			Injectable: true, Iface: "example.com/paysvc/bus.Publisher"},
	)
	f.States = append(f.States, fact.StateMachine{
		Type:   "example.com/paysvc/domain.SettlementMode",
		Field:  "Mode",
		States: []string{"ModeAccepted", "ModeQueued", "ModeSent"},
		Writes: []fact.StateWrite{{In: process, To: "ModeQueued", Line: 45, InTx: true}},
	})
	return f
}
