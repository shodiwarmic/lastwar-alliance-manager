// eligibility.go — reward-tier eligibility (private-docs 284, decision 10). Pure.
//
// A tier may require a number of wars and the Core events. Per member and tier:
//
//   - wars: On Pace when attended ≥ required × completed ÷ scheduled; Can Still Qualify when
//     attended + (scheduled − completed) ≥ required; Eliminated otherwise. Once the season's
//     last scheduled war is completed (or the season is archived) On Pace becomes Eligible.
//   - Core events: each required event of a completed week counts — Full or Completed attended,
//     Excused or N/A left out, Partial or Absent missed, nothing recorded unknown (listed). A
//     miss eliminates, an unknown leaves the member able to qualify, an exemption satisfies it.
//
// The tier's status is the worst of its rules; a tier with no rule is not applicable. Eligibility
// never assigns — it is shown beside the officers' choice.

package app

import "fmt"

// tierRule is what eligibility reads of a tier.
type tierRule struct {
	Key          string
	Label        string
	WarsRequired *int
	RequiresCore bool
}

// eligibilityInput is one member's figures.
type eligibilityInput struct {
	HasWars       bool
	WarsAttended  int
	WarsCompleted int
	WarsScheduled int
	SeasonOver    bool
	CoreRequired  int // required events of completed weeks, less Excused / N/A
	CoreAttended  int
	CoreMissed    []string // labels
	CoreUnknown   []string // labels
	Exempt        bool
	ExemptNote    string
}

type tierEligibility struct {
	Tier    string   `json:"tier"`
	Status  string   `json:"status"` // eligible | on_pace | can_still_qualify | eliminated | not_applicable
	Reasons []string `json:"reasons"`
}

var eligibilitySeverity = map[string]int{"eligible": 0, "on_pace": 1, "can_still_qualify": 2, "eliminated": 3}

// evaluateTier judges one member against one tier.
func evaluateTier(rule tierRule, in eligibilityInput) tierEligibility {
	out := tierEligibility{Tier: rule.Key, Status: "not_applicable", Reasons: []string{}}
	worst := func(s string) {
		if out.Status == "not_applicable" || eligibilitySeverity[s] > eligibilitySeverity[out.Status] {
			out.Status = s
		}
	}
	if rule.WarsRequired != nil {
		req := *rule.WarsRequired
		switch {
		case !in.HasWars:
			out.Reasons = append(out.Reasons, "wars: this season has no war records")
		case in.WarsScheduled <= 0:
			out.Reasons = append(out.Reasons, "wars: the season's scheduled war total is not set")
		default:
			att, done, sched := in.WarsAttended, in.WarsCompleted, in.WarsScheduled
			left := sched - done
			if left < 0 {
				left = 0
			}
			var status string
			switch {
			case in.SeasonOver || left == 0:
				status = "eliminated"
				if att >= req {
					status = "eligible"
				}
			case att*sched >= req*done:
				status = "on_pace"
			case att+left >= req:
				status = "can_still_qualify"
			default:
				status = "eliminated"
			}
			worst(status)
			out.Reasons = append(out.Reasons, fmt.Sprintf("wars: attended %d of %d completed (%d scheduled, %d required)", att, done, sched, req))
		}
	}
	if rule.RequiresCore {
		switch {
		case in.Exempt:
			s := "on_pace"
			if in.SeasonOver {
				s = "eligible"
			}
			worst(s)
			r := "Core events: exempt"
			if in.ExemptNote != "" {
				r += " (" + in.ExemptNote + ")"
			}
			out.Reasons = append(out.Reasons, r)
		case len(in.CoreMissed) > 0:
			worst("eliminated")
			out.Reasons = append(out.Reasons, fmt.Sprintf("Core events: missed %s", joinLabels(in.CoreMissed)))
		case len(in.CoreUnknown) > 0:
			worst("can_still_qualify")
			out.Reasons = append(out.Reasons, fmt.Sprintf("Core events: %d of %d attended; not recorded: %s",
				in.CoreAttended, in.CoreRequired, joinLabels(in.CoreUnknown)))
		default:
			s := "on_pace"
			if in.SeasonOver {
				s = "eligible"
			}
			worst(s)
			out.Reasons = append(out.Reasons, fmt.Sprintf("Core events: %d of %d attended", in.CoreAttended, in.CoreRequired))
		}
	}
	return out
}

func joinLabels(ls []string) string {
	out := ""
	for i, l := range ls {
		if i > 0 {
			out += ", "
		}
		out += l
	}
	return out
}

// countsAsEligible is whether a status counts toward a tier's "eligible" figure.
func countsAsEligible(status string) bool { return status == "eligible" || status == "on_pace" }

// coreEventInput reads a member's Core figures from the required events of completed weeks and
// their entries (event id → result).
func coreEventInput(events []weekEvent, completedWeeks int, results map[int]string) (required, attended int, missed, unknown []string) {
	missed, unknown = []string{}, []string{}
	for _, e := range events {
		if !e.RequiredForCore || e.WeekNumber > completedWeeks {
			continue
		}
		label := fmt.Sprintf("W%d %s", e.WeekNumber, e.Label)
		switch results[e.ID] {
		case "full", "completed":
			required++
			attended++
		case "excused", "na":
		case "partial", "absent":
			required++
			missed = append(missed, label)
		default:
			required++
			unknown = append(unknown, label)
		}
	}
	return required, attended, missed, unknown
}
