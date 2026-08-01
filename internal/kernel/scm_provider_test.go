package kernel_test

import (
	"context"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/scm/write"
)

type stubPusher struct {
	name string
	seen []write.PushRequest
}

func (s *stubPusher) PushBranch(_ context.Context, req write.PushRequest) (write.Receipt, error) {
	s.seen = append(s.seen, req)
	return write.Receipt{AfterSHA: req.NewSHA}, nil
}

func TestSelectSCMProvider_FailClosed(t *testing.T) {
	reg := kernel.NewSCMWriterRegistry()
	_ = reg.Register(kernel.SCMProviderGitHub, &stubPusher{name: "gh"})
	_ = reg.Register(kernel.SCMProviderBitbucket, &stubPusher{name: "bb"})

	cases := []struct {
		name     string
		provider string
		digest   string
		remote   string
		code     string
	}{
		{name: "missing provider", provider: "", digest: "d1", remote: "", code: string(kernel.ResultSCMProviderMissing)},
		{name: "absent policy", provider: "github", digest: "", remote: "", code: string(kernel.ResultSCMPolicyAbsent)},
		{name: "unknown", provider: "gitlab", digest: "d1", remote: "", code: string(kernel.ResultSCMProviderUnknown)},
		{name: "mismatch", provider: "github", digest: "d1", remote: "https://bitbucket.org/w/r.git", code: string(kernel.ResultSCMProviderMismatch)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, code, err := kernel.SelectSCMProvider(tc.provider, tc.digest, tc.remote, reg)
			if err == nil {
				t.Fatal("expected error")
			}
			if string(code) != tc.code {
				t.Fatalf("code = %s, want %s", code, tc.code)
			}
		})
	}
}

func TestSelectSCMProvider_ByPolicyAlone(t *testing.T) {
	gh := &stubPusher{name: "gh"}
	bb := &stubPusher{name: "bb"}
	reg := kernel.NewSCMWriterRegistry()
	_ = reg.Register(kernel.SCMProviderGitHub, gh)
	_ = reg.Register(kernel.SCMProviderBitbucket, bb)

	sel, code, err := kernel.SelectSCMProvider("bitbucket", "digest-abc", "https://bitbucket.org/w/r.git", reg)
	if err != nil || code != "" {
		t.Fatalf("select: %v code=%s", err, code)
	}
	if sel.Provider != kernel.SCMProviderBitbucket || sel.PolicyDigest != "digest-abc" {
		t.Fatalf("sel = %+v", sel)
	}
	req := write.PushRequest{RepoPath: "/tmp", Branch: "main", NewSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if _, err := kernel.PushBranchSelected(context.Background(), reg, sel, req); err != nil {
		t.Fatal(err)
	}
	if len(bb.seen) != 1 || len(gh.seen) != 0 {
		t.Fatalf("writer routing wrong: gh=%d bb=%d", len(gh.seen), len(bb.seen))
	}
}

func TestSelectSCMProvider_MissingWriter(t *testing.T) {
	reg := kernel.NewSCMWriterRegistry()
	_, code, err := kernel.SelectSCMProvider("github", "d", "", reg)
	if err == nil || code != kernel.ResultSCMWriterMissing {
		t.Fatalf("got code=%s err=%v", code, err)
	}
}
