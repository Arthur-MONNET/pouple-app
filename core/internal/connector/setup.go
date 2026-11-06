package connector

// Gap is something the setup still lacks: a required connector, or one connector among alternatives.
type Gap struct {
	// Names are the display names, in setup order: one for a required connector, several for a choice.
	Names []string
}

// SetupGaps lists what must still be connected: every required connector, and one connector for each choice group.
// The statuses are in setup order, as Registry.List returns them.
func SetupGaps(statuses []Status) []Gap {
	var gaps []Gap
	groups := map[string]*Gap{}
	connectedGroup := map[string]bool{}
	var order []string
	for _, status := range statuses {
		d := status.Descriptor
		switch {
		case d.Choice != "":
			if groups[d.Choice] == nil {
				groups[d.Choice] = &Gap{}
				order = append(order, d.Choice)
			}
			groups[d.Choice].Names = append(groups[d.Choice].Names, d.Name)
			connectedGroup[d.Choice] = connectedGroup[d.Choice] || status.Connected
		case d.Required && !status.Connected:
			gaps = append(gaps, Gap{Names: []string{d.Name}})
		}
	}
	for _, choice := range order {
		if !connectedGroup[choice] {
			gaps = append(gaps, *groups[choice])
		}
	}
	return gaps
}

// SetupComplete is true when nothing is lacking: Slack is connected and so is at least one AI.
func SetupComplete(statuses []Status) bool { return len(SetupGaps(statuses)) == 0 }
