package agent

import "github.com/katayunak/testigo/internal/scanningFlow/flowEntity"

func batchedFixture() *flowEntity.Flow {
	f := fixtureFlow()
	process := f.Seams[0].In
	f.Seams = append(f.Seams,
		flowEntity.Seam{In: process, Kind: flowEntity.SeamHTTP,
			Target: "(example.com/paysvc/psp.Gateway).Capture", Line: 71,
			Injectable: true, Iface: "example.com/paysvc/psp.Gateway"},
		flowEntity.Seam{In: process, Kind: flowEntity.SeamQueue,
			Target: "(example.com/paysvc/bus.Publisher).Publish", Line: 80,
			Injectable: true, Iface: "example.com/paysvc/bus.Publisher"},
	)
	f.States = append(f.States, flowEntity.StateMachine{
		Type:   "example.com/paysvc/domain.SettlementMode",
		Field:  "Mode",
		States: []string{"ModeAccepted", "ModeQueued", "ModeSent"},
		Writes: []flowEntity.StateWrite{{In: process, To: "ModeQueued", Line: 45, InTx: true}},
	})
	return f
}
