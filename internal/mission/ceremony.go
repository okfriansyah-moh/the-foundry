package mission

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const readinessChecklistPath = "config/ceremony-checklist.yaml"

const (
	ReadinessPass = "pass"
	ReadinessFail = "fail"
)

type CeremonyChecklist struct {
	Groups []CeremonyGroup `yaml:"groups" json:"groups"`
}

type CeremonyGroup struct {
	Name  string         `yaml:"name" json:"name"`
	Items []CeremonyItem `yaml:"items" json:"items"`
}

type CeremonyItem struct {
	Key      string `yaml:"key" json:"key"`
	Prompt   string `yaml:"prompt" json:"prompt"`
	Required bool   `yaml:"required" json:"required"`
}

type CeremonyAnswer struct {
	Resolved    bool   `yaml:"resolved" json:"resolved"`
	Evidence    string `yaml:"evidence" json:"evidence"`
	Deferred    bool   `yaml:"deferred" json:"deferred"`
	Reason      string `yaml:"reason" json:"reason"`
	RevisitWhen string `yaml:"revisit_when" json:"revisit_when"`
	Principal   string `yaml:"principal" json:"principal"`
}

type CompletedGate struct {
	Gate        string    `yaml:"gate" json:"gate"`
	Evidence    string    `yaml:"evidence" json:"evidence"`
	CompletedAt time.Time `yaml:"completed_at" json:"completed_at"`
	Principal   string    `yaml:"principal" json:"principal"`
}

type DeferredGate struct {
	Gate        string `yaml:"gate" json:"gate"`
	Reason      string `yaml:"reason" json:"reason"`
	RevisitWhen string `yaml:"revisit_when" json:"revisit_when"`
}

type MissionReadinessArtifact struct {
	MissionID      string          `yaml:"mission_id" json:"mission_id"`
	CompletedGates []CompletedGate `yaml:"completed_gates" json:"completed_gates"`
	DeferredGates  []DeferredGate  `yaml:"deferred_gates" json:"deferred_gates"`
	Readiness      string          `yaml:"readiness" json:"readiness"`
	ApprovedBy     string          `yaml:"approved_by" json:"approved_by"`
	Digest         string          `yaml:"digest" json:"digest"`
}

func (a MissionReadinessArtifact) IsPassing() bool {
	return strings.EqualFold(a.Readiness, ReadinessPass)
}

func LoadCeremonyChecklist(path string) (CeremonyChecklist, error) {
	if strings.TrimSpace(path) == "" {
		path = readinessChecklistPath
	}
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return CeremonyChecklist{}, fmt.Errorf("mission ceremony: read checklist %s: %w", path, err)
	}
	var c CeremonyChecklist
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return CeremonyChecklist{}, fmt.Errorf("mission ceremony: decode checklist %s: %w", path, err)
	}
	for gi := range c.Groups {
		for ii := range c.Groups[gi].Items {
			if !c.Groups[gi].Items[ii].Required {
				// The authoritative ceremony list treats every item as a gate.
				c.Groups[gi].Items[ii].Required = true
			}
		}
	}
	return c, nil
}

func BuildMissionReadinessArtifact(missionID, approvedBy string, checklist CeremonyChecklist, answers map[string]CeremonyAnswer, gateEvents []GateEvent, now time.Time) (MissionReadinessArtifact, error) {
	if missionID == "" {
		return MissionReadinessArtifact{}, fmt.Errorf("mission ceremony: mission_id is required")
	}
	if strings.TrimSpace(approvedBy) == "" {
		return MissionReadinessArtifact{}, fmt.Errorf("mission ceremony: approved_by is required")
	}
	var completed []CompletedGate
	var deferred []DeferredGate
	readiness := ReadinessPass

	for _, group := range checklist.Groups {
		for _, item := range group.Items {
			answer, ok := answers[item.Key]
			if !ok {
				return MissionReadinessArtifact{}, fmt.Errorf("mission ceremony: missing checklist answer for %q", item.Key)
			}
			if answer.Resolved && answer.Deferred {
				return MissionReadinessArtifact{}, fmt.Errorf("mission ceremony: checklist answer %q cannot be both resolved and deferred", item.Key)
			}
			if !answer.Resolved && !answer.Deferred {
				return MissionReadinessArtifact{}, fmt.Errorf("mission ceremony: checklist answer %q must be resolved or deferred", item.Key)
			}
			if answer.Resolved {
				if strings.TrimSpace(answer.Evidence) == "" {
					return MissionReadinessArtifact{}, fmt.Errorf("mission ceremony: checklist answer %q resolved without evidence", item.Key)
				}
				principal := strings.TrimSpace(answer.Principal)
				if principal == "" {
					principal = approvedBy
				}
				completed = append(completed, CompletedGate{
					Gate:        item.Key,
					Evidence:    answer.Evidence,
					CompletedAt: now.UTC(),
					Principal:   principal,
				})
				continue
			}
			if strings.TrimSpace(answer.Reason) == "" || strings.TrimSpace(answer.RevisitWhen) == "" {
				return MissionReadinessArtifact{}, fmt.Errorf("mission ceremony: checklist answer %q deferred without reason/revisit_when", item.Key)
			}
			deferred = append(deferred, DeferredGate{
				Gate:        item.Key,
				Reason:      answer.Reason,
				RevisitWhen: answer.RevisitWhen,
			})
			if item.Required {
				readiness = ReadinessFail
			}
		}
	}

	// Any unresolved unforeseen gate discovered during execution is carried
	// into the next ceremony as an explicit deferred gate.
	sort.SliceStable(gateEvents, func(i, j int) bool {
		return gateEvents[i].OccurredAt.Before(gateEvents[j].OccurredAt)
	})
	for _, ev := range gateEvents {
		if ev.ResolvedAt != nil {
			continue
		}
		readiness = ReadinessFail
		deferred = append(deferred, DeferredGate{
			Gate:        "unforeseen-human-gate:" + ev.Action,
			Reason:      "encountered during runtime; must be closed before unattended missions",
			RevisitWhen: "next-ceremony",
		})
	}

	artifact := MissionReadinessArtifact{
		MissionID:      missionID,
		CompletedGates: completed,
		DeferredGates:  deferred,
		Readiness:      readiness,
		ApprovedBy:     approvedBy,
	}
	digest, err := readinessDigest(artifact)
	if err != nil {
		return MissionReadinessArtifact{}, err
	}
	artifact.Digest = digest
	return artifact, nil
}

func readinessDigest(artifact MissionReadinessArtifact) (string, error) {
	copyArtifact := artifact
	copyArtifact.Digest = ""
	raw, err := json.Marshal(copyArtifact)
	if err != nil {
		return "", fmt.Errorf("mission ceremony: marshal readiness artifact digest input: %w", err)
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
