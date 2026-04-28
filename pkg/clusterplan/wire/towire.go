package wire

import (
	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	rkeplan "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1/plan"
)

// ToWire projects a NodePlanSpec onto the existing system-agent wire
// format. Outputs are NOT transmitted — they are framework-side
// declarations consumed by FromWireStatus / ExtractOutputs.
//
// Field-mapping notes (what's lost or transformed):
//
//   - File.Drain → File.Minor (inverted: Drain=true means "drain on change",
//     which the agent expresses as Minor=false). The default Drain=false
//     therefore becomes Minor=true (no drain).
//
//   - Instruction.Strategy fields are mostly NOT representable in the
//     existing wire format. SuccessThreshold / FailureThreshold /
//     Idempotent / RetryOnStartup / TimeoutSeconds are dropped at this
//     layer — they will become load-bearing once the agent reads the
//     NodePlan natively (PR6).
//
//   - Instruction.Strategy.PeriodSeconds > 0 routes the instruction into
//     PeriodicInstructions instead of one-time Instructions.
//
//   - Probe maps RetryStrategy{InitialDelaySeconds,TimeoutSeconds,
//     SuccessThreshold,FailureThreshold} and HTTPGetAction directly. The
//     wire format keys probes by name; ToWire uses Probe.Name as the
//     map key. Empty Probe.Name causes ToWire to error.
func ToWire(spec v1alpha1.NodePlanSpec) (rkeplan.NodePlan, error) {
	out := rkeplan.NodePlan{}

	if len(spec.Files) > 0 {
		out.Files = make([]rkeplan.File, 0, len(spec.Files))
		for _, f := range spec.Files {
			out.Files = append(out.Files, rkeplan.File{
				Content:     f.Content,
				Path:        f.Path,
				Permissions: f.Permissions,
				Minor:       !f.Drain,
			})
		}
	}

	for _, ins := range spec.Instructions {
		if ins.Strategy.PeriodSeconds > 0 {
			out.PeriodicInstructions = append(out.PeriodicInstructions, rkeplan.PeriodicInstruction{
				Name:          ins.Name,
				Image:         ins.Image,
				Env:           append([]string(nil), ins.Env...),
				Args:          append([]string(nil), ins.Args...),
				Command:       ins.Command,
				PeriodSeconds: ins.Strategy.PeriodSeconds,
			})
			continue
		}
		out.Instructions = append(out.Instructions, rkeplan.OneTimeInstruction{
			Name:       ins.Name,
			Image:      ins.Image,
			Env:        append([]string(nil), ins.Env...),
			Args:       append([]string(nil), ins.Args...),
			Command:    ins.Command,
			SaveOutput: ins.SaveOutput,
		})
	}

	if len(spec.Probes) > 0 {
		out.Probes = make(map[string]rkeplan.Probe, len(spec.Probes))
		for _, p := range spec.Probes {
			if p.Name == "" {
				return rkeplan.NodePlan{}, errEmptyProbeName
			}
			wp := rkeplan.Probe{
				Name:                p.Name,
				InitialDelaySeconds: p.RetryStrategy.InitialDelaySeconds,
				TimeoutSeconds:      p.RetryStrategy.TimeoutSeconds,
				SuccessThreshold:    p.RetryStrategy.SuccessThreshold,
				FailureThreshold:    p.RetryStrategy.FailureThreshold,
			}
			if p.HTTPGetAction != nil {
				wp.HTTPGetAction = rkeplan.HTTPGetAction{
					URL:        p.HTTPGetAction.URL,
					Insecure:   p.HTTPGetAction.Insecure,
					ClientCert: p.HTTPGetAction.ClientCert,
					ClientKey:  p.HTTPGetAction.ClientKey,
					CACert:     p.HTTPGetAction.CACert,
				}
			}
			out.Probes[p.Name] = wp
		}
	}

	return out, nil
}
