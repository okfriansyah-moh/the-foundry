package synthetic

import "context"

type Check interface {
	Name() string
	Run(ctx context.Context) error
}

type Result struct {
	Check  string `json:"check"`
	Passed bool   `json:"passed"`
	Error  string `json:"error,omitempty"`
}

type BatteryResult struct {
	Mode    VerificationMode `json:"mode"`
	Results []Result         `json:"results"`
}

func (r BatteryResult) Passed() bool {
	for _, c := range r.Results {
		if !c.Passed {
			return false
		}
	}
	return true
}

func RunBattery(ctx context.Context, mode VerificationMode, checks []Check) BatteryResult {
	out := BatteryResult{
		Mode:    mode,
		Results: make([]Result, 0, len(checks)),
	}
	for _, chk := range checks {
		err := chk.Run(ctx)
		res := Result{Check: chk.Name(), Passed: err == nil}
		if err != nil {
			res.Error = err.Error()
		}
		out.Results = append(out.Results, res)
	}
	return out
}
