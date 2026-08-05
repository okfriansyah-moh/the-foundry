/* Delivery Foundry Operator Console — mock data + trust-domain scoping
   Mirrors codebase: one FOUNDRY_PROFILE / foundryd process = one trust domain.
   Multi-profile = multi-process (separate deployments). UI never mixes them. */
(function () {
  "use strict";

  const ROOT = document.body.dataset.root || ".";
  const STORAGE_KEY = "foundry_mock_deployment";

  const DEPLOYMENTS = [
    {
      id: "personal-autonomous-venture",
      short: "Venture",
      track: "A · Personal Autonomous Venture",
      api: "venture.foundry.local:8080",
      max_active_missions: 2,
      monthly_cap_usd: 200,
      spent_usd: 170,
    },
    {
      id: "organization-10x",
      short: "Org 10x",
      track: "B · Organization 10x",
      api: "org10x.foundry.local:8080",
      max_active_missions: 4,
      monthly_cap_usd: 2000,
      spent_usd: 640,
    },
  ];

  function resolveActiveProfile() {
    const fromUrl = new URLSearchParams(location.search).get("deployment");
    if (fromUrl && DEPLOYMENTS.some((d) => d.id === fromUrl)) {
      try {
        localStorage.setItem(STORAGE_KEY, fromUrl);
      } catch (_) {}
      return fromUrl;
    }
    try {
      const stored = localStorage.getItem(STORAGE_KEY);
      if (stored && DEPLOYMENTS.some((d) => d.id === stored)) return stored;
    } catch (_) {}
    return "personal-autonomous-venture";
  }

  /* CAP-01–04 / M8: catalogs + enablement mock as DB SoT (seeded from YAML; Tasks 156–161) */
  const AGENT_CATALOG = [
    {
      name: "planning",
      description: "SPEC/requirements → executable PLAN",
      skills: ["guardrails", "stop-slop", "foundry-planning"],
      writes: false,
      sha256: "sha256:plan…01",
      sot: "db",
    },
    {
      name: "pec",
      description: "Proposes waves only — never decides (C5)",
      skills: ["guardrails", "stop-slop"],
      writes: true,
      sha256: "sha256:pec…02",
      sot: "db",
    },
    {
      name: "implementation",
      description: "Bounded task implementer",
      skills: ["guardrails", "stop-slop", "task-implementation"],
      writes: true,
      sha256: "sha256:impl…03",
      sot: "db",
    },
    {
      name: "backend",
      description: "Backend/API/DB bounded tasks",
      skills: ["guardrails", "stop-slop", "task-implementation", "testing"],
      writes: true,
      sha256: "sha256:back…04",
      sot: "db",
    },
    {
      name: "reviewer",
      description: "Independent review — never implementer",
      skills: [
        "guardrails",
        "stop-slop",
        "code-reviewer-correctness",
        "code-reviewer-quality",
        "code-reviewer-security",
        "sonarqube-quality-gate",
      ],
      writes: false,
      sha256: "sha256:rev…05",
      sot: "db",
    },
    {
      name: "verification",
      description: "Risk-based deterministic checks",
      skills: ["guardrails", "stop-slop", "testing"],
      writes: false,
      sha256: "sha256:ver…06",
      sot: "db",
    },
  ];

  const SKILL_CATALOG = [
    {
      name: "guardrails",
      kind: "skill",
      description: "Constitution, policy, side-effect boundaries",
      sha256: "sha256:sk…g1",
      sot: "db",
    },
    {
      name: "stop-slop",
      kind: "skill",
      description: "No unverified done / silent scope drift",
      sha256: "sha256:sk…s2",
      sot: "db",
    },
    {
      name: "principal-architect",
      kind: "skill",
      description: "Smallest compliant architecture",
      sha256: "sha256:sk…p3",
      sot: "db",
    },
    {
      name: "task-implementation",
      kind: "skill",
      description: "One approved task + paired tests",
      sha256: "sha256:sk…t4",
      sot: "db",
    },
    {
      name: "foundry-planning",
      kind: "skill",
      description: "Dependency-ordered executable plans",
      sha256: "sha256:sk…f5",
      sot: "db",
    },
    {
      name: "code-reviewer-correctness",
      kind: "skill",
      description: "Behavior, concurrency, regressions",
      sha256: "sha256:sk…c6",
      sot: "db",
    },
    {
      name: "code-reviewer-quality",
      kind: "skill",
      description: "Maintainability and scope discipline",
      sha256: "sha256:sk…q7",
      sot: "db",
    },
    {
      name: "code-reviewer-security",
      kind: "skill",
      description: "Trust boundaries and authz",
      sha256: "sha256:sk…s8",
      sot: "db",
    },
    {
      name: "sonarqube-quality-gate",
      kind: "skill",
      description: "Quality-gate interpretation",
      sha256: "sha256:sk…sq",
      sot: "db",
    },
    {
      name: "testing",
      kind: "skill",
      description: "Deterministic checks by risk",
      sha256: "sha256:sk…te",
      sot: "db",
    },
    {
      name: "commercial-readiness",
      kind: "domain",
      description: "Venture launch-readiness evidence",
      sha256: "sha256:sk…cr",
      sot: "db",
    },
    {
      name: "release-verification",
      kind: "domain",
      description: "Release handoff without merge/deploy authority",
      sha256: "sha256:sk…rv",
      sot: "db",
    },
  ];

  /* M8 CFG mock tables — Postgres SoT; platform ceilings stay YAML */
  const DB_POLICY_LAYERS = [
    {
      key: "scm.write",
      value: "kernel-only",
      layer: "platform",
      sot: "yaml",
      editable: false,
      ceiling: true,
    },
    {
      key: "approve.high_risk",
      value: "webauthn",
      layer: "platform",
      sot: "yaml",
      editable: false,
      ceiling: true,
    },
    {
      key: "org.max_concurrent_waves",
      value: "4",
      layer: "org",
      sot: "db",
      editable: true,
      ceiling: false,
    },
    {
      key: "profile.executor_prefer",
      value: "claude-code",
      layer: "profile",
      sot: "db",
      editable: true,
      ceiling: false,
    },
  ];

  const DB_QUOTAS = [
    { key: "max_workflows", value: "32", scope: "org", sot: "db" },
    { key: "max_runners", value: "8", scope: "org", sot: "db" },
    { key: "max_active_missions", value: "2", scope: "profile", sot: "db" },
    { key: "monthly_cap_usd", value: "200", scope: "profile", sot: "db" },
  ];

  const DB_MISSION_DECIDE = [
    {
      key: "self_classify",
      value: "false",
      sot: "yaml",
      editable: false,
      note: "C6 floor — not writable",
    },
    {
      key: "discrepancy_raises_tier",
      value: "true",
      sot: "db",
      editable: true,
      note: "operator-hot",
    },
    {
      key: "stale_gate_hours",
      value: "48",
      sot: "db",
      editable: true,
      note: "operator-hot",
    },
  ];

  const DB_RATES = [
    {
      model: "claude-sonnet-4",
      input_per_mtok: "3.00",
      output_per_mtok: "15.00",
      version: 3,
      sot: "db",
    },
    {
      model: "gpt-4.1",
      input_per_mtok: "2.00",
      output_per_mtok: "8.00",
      version: 2,
      sot: "db",
    },
    {
      model: "gemini-2.5-pro",
      input_per_mtok: "1.25",
      output_per_mtok: "10.00",
      version: 1,
      sot: "db",
    },
  ];

  const DB_TUNABLES = [
    {
      key: "wave_concurrency",
      value: "2",
      bound_min: "1",
      bound_max: "8",
      bounds_sot: "yaml",
      value_sot: "db",
    },
    {
      key: "retry_max",
      value: "3",
      bound_min: "1",
      bound_max: "5",
      bounds_sot: "yaml",
      value_sot: "db",
    },
    {
      key: "research_max_uses",
      value: "12",
      bound_min: "1",
      bound_max: "40",
      bounds_sot: "yaml",
      value_sot: "db",
    },
  ];

  const DB_OPPORTUNITY = [
    { key: "weight.market", value: "0.28", sot: "db", editable: true },
    { key: "weight.feasibility", value: "0.22", sot: "db", editable: true },
    { key: "weight.risk", value: "0.18", sot: "db", editable: true },
    { key: "weight.timing", value: "0.16", sot: "db", editable: true },
    { key: "weight.moat", value: "0.16", sot: "db", editable: true },
    {
      key: "research.domains",
      value: "config/opportunity-research-domains.yaml",
      sot: "yaml",
      editable: false,
    },
  ];

  const BINDINGS = [
    {
      name: "product-implementation",
      implementer: "implementation",
      reviewer: "reviewer",
    },
    {
      name: "backend-implementation",
      implementer: "backend",
      reviewer: "reviewer",
    },
  ];

  const PROFILE_ENABLE = {
    "personal-autonomous-venture": {
      agents: [
        "planning",
        "pec",
        "implementation",
        "backend",
        "reviewer",
        "verification",
      ],
      skills: [
        "guardrails",
        "stop-slop",
        "principal-architect",
        "task-implementation",
        "foundry-planning",
        "code-reviewer-correctness",
        "code-reviewer-quality",
        "code-reviewer-security",
        "sonarqube-quality-gate",
        "testing",
      ],
      domain_skills: ["commercial-readiness", "release-verification"],
      product_path: "api-changelog-assistant/.foundry/skills/enabled.yaml",
      install: {
        provider: "claude-code",
        status: "installed",
        doctor: "ok",
        last_install: "2026-08-03T18:22:00Z",
        digest: "sha256:a1b2c3d4e5f60718",
        files: 28,
      },
      evolve: [
        {
          skill: "task-implementation",
          version: 2,
          previous: 1,
          state: "active",
          sha256: "sha256:91aa…f3",
          promoted_at: "2026-08-03T16:10:00Z",
        },
        {
          skill: "guardrails",
          version: 1,
          previous: null,
          state: "active",
          sha256: "sha256:10be…c2",
          promoted_at: "2026-07-28T09:00:00Z",
        },
      ],
      proposals: [],
    },
    "organization-10x": {
      agents: ["planning", "implementation", "reviewer", "verification"],
      skills: [
        "guardrails",
        "stop-slop",
        "task-implementation",
        "foundry-planning",
        "code-reviewer-correctness",
        "code-reviewer-quality",
        "code-reviewer-security",
        "testing",
      ],
      domain_skills: ["release-verification"],
      product_path: "org-initiative/.foundry/skills/enabled.yaml",
      install: {
        provider: "claude-code",
        status: "installed",
        doctor: "warn",
        last_install: "2026-08-02T11:04:00Z",
        digest: "sha256:88ee9911aabbccdd",
        files: 19,
        doctor_note: "1 enabled file missing — reinstall required (mock)",
      },
      evolve: [],
      proposals: [
        {
          skill: "testing",
          proposed_version: 2,
          state: "proposal-only",
          reason: "Org L1 cannot auto-activate (H)",
          created_at: "2026-08-03T12:00:00Z",
        },
      ],
    },
  };

  window.FoundryMock = {
    deployments: DEPLOYMENTS,
    activeProfile: resolveActiveProfile(),
    agentCatalog: AGENT_CATALOG,
    skillCatalog: SKILL_CATALOG,
    bindings: BINDINGS,
    profileEnable: PROFILE_ENABLE,
    dbPolicyLayers: DB_POLICY_LAYERS,
    dbQuotas: DB_QUOTAS,
    dbMissionDecide: DB_MISSION_DECIDE,
    dbRates: DB_RATES,
    dbTunables: DB_TUNABLES,
    dbOpportunity: DB_OPPORTUNITY,

    packagingForActive() {
      return (
        PROFILE_ENABLE[this.activeProfile] ||
        PROFILE_ENABLE["personal-autonomous-venture"]
      );
    },

    validatePackaging() {
      const en = this.packagingForActive();
      const agentNames = new Set(AGENT_CATALOG.map((a) => a.name));
      const skillNames = new Set(SKILL_CATALOG.map((s) => s.name));
      const unknown = [];
      en.agents.forEach((n) => {
        if (!agentNames.has(n)) unknown.push("agent:" + n);
      });
      en.skills.forEach((n) => {
        if (!skillNames.has(n)) unknown.push("skill:" + n);
      });
      en.domain_skills.forEach((n) => {
        if (!skillNames.has(n)) unknown.push("domain:" + n);
      });
      const bindingOk = BINDINGS.every((b) => b.implementer !== b.reviewer);
      return {
        ok: unknown.length === 0 && bindingOk,
        unknown,
        bindingOk,
        message:
          unknown.length === 0 && bindingOk
            ? "Validate OK — enabled ⊆ catalog; reviewer ≠ implementer"
            : "Validate FAILED — refuse install (fail closed)",
      };
    },

    workflows: [
      {
        id: "wf-7a2c91",
        title: "Venture delivery · checkout polish",
        status: "WAITING",
        phase: "reviewing",
        reason: "human-approval",
        result_code: null,
        profile: "personal-autonomous-venture",
        mission_id: "msn-01",
        updated: "2026-08-03T01:12:00Z",
      },
      {
        id: "wf-3bf104",
        title: "Mission pause · unforeseen gate",
        status: "WAITING",
        phase: "observing",
        reason: "unforeseen-human-gate",
        result_code: null,
        profile: "personal-autonomous-venture",
        mission_id: "msn-01",
        updated: "2026-08-03T00:48:00Z",
      },
      {
        id: "wf-9e11aa",
        title: "Budget hold · monthly cap",
        status: "WAITING",
        phase: "implementation",
        reason: "budget",
        result_code: null,
        profile: "personal-autonomous-venture",
        mission_id: "msn-02",
        updated: "2026-08-02T22:10:00Z",
      },
      {
        id: "wf-c80de2",
        title: "Spec → plan · intake product",
        status: "RUNNING",
        phase: "planning",
        reason: null,
        result_code: null,
        profile: "personal-autonomous-venture",
        mission_id: "msn-01",
        updated: "2026-08-03T01:40:00Z",
      },
      {
        id: "wf-0aa991",
        title: "Plan admit · routing change",
        status: "PENDING",
        phase: "admission",
        reason: null,
        result_code: null,
        profile: "personal-autonomous-venture",
        mission_id: null,
        updated: "2026-08-03T01:44:00Z",
      },
      {
        id: "wf-88f001",
        title: "Deploy canary · Fly personal",
        status: "SUCCEEDED",
        phase: "deploying",
        reason: null,
        result_code: null,
        profile: "personal-autonomous-venture",
        mission_id: "msn-01",
        updated: "2026-08-02T18:22:00Z",
      },
      {
        id: "wf-kill09",
        title: "Mission kill · policy terminate",
        status: "CANCELLED",
        phase: "curating",
        reason: null,
        result_code: "MISSION_TERMINATED_BY_POLICY",
        profile: "personal-autonomous-venture",
        mission_id: "msn-03",
        updated: "2026-08-01T12:00:00Z",
      },
      {
        id: "wf-mrr100",
        title: "Target reached · maintenance",
        status: "SUCCEEDED",
        phase: "observing",
        reason: null,
        result_code: "MISSION_TARGET_REACHED",
        profile: "personal-autonomous-venture",
        mission_id: "msn-02",
        updated: "2026-07-28T09:00:00Z",
      },
      {
        id: "wf-opp91",
        title: "Opportunity verdict · build nothing",
        status: "SUCCEEDED",
        phase: "intake",
        reason: null,
        result_code: "OPPORTUNITY_REJECTED",
        profile: "personal-autonomous-venture",
        mission_id: "msn-01",
        updated: "2026-07-20T14:30:00Z",
      },
      {
        id: "wf-adm-v",
        title: "Admission reject · self-classify",
        status: "FAILED",
        phase: "admission",
        reason: "admission-rejected",
        result_code: "ADMISSION_REJECTED",
        profile: "personal-autonomous-venture",
        mission_id: null,
        updated: "2026-08-02T16:01:00Z",
      },
      /* organization-10x deployment only — other foundryd process */
      {
        id: "wf-5510cc",
        title: "Provider capacity · Claude wave",
        status: "WAITING",
        phase: "implementation",
        reason: "provider-capacity",
        result_code: null,
        profile: "organization-10x",
        mission_id: "msn-10x-01",
        updated: "2026-08-02T21:05:00Z",
      },
      {
        id: "wf-1120ab",
        title: "Evidence verify · Task 36 slice",
        status: "RUNNING",
        phase: "verifying",
        reason: null,
        result_code: null,
        profile: "organization-10x",
        mission_id: "msn-10x-01",
        updated: "2026-08-03T01:35:00Z",
      },
      {
        id: "wf-dead42",
        title: "Admission reject · dependency change",
        status: "FAILED",
        phase: "admission",
        reason: "admission-rejected",
        result_code: "ADMISSION_REJECTED",
        profile: "organization-10x",
        mission_id: null,
        updated: "2026-08-02T16:01:00Z",
      },
      {
        id: "wf-10x-hand",
        title: "10x branch handoff ready",
        status: "SUCCEEDED",
        phase: "integrating",
        reason: null,
        result_code: "TEN_X_BRANCH_HANDOFF_READY",
        profile: "organization-10x",
        mission_id: "msn-10x-01",
        updated: "2026-08-01T11:00:00Z",
      },
      {
        id: "wf-10x-sec",
        title: "Security hold · org policy",
        status: "WAITING",
        phase: "reviewing",
        reason: "security-hold",
        result_code: null,
        profile: "organization-10x",
        mission_id: "msn-10x-02",
        updated: "2026-08-03T00:10:00Z",
      },
    ],

    missions: [
      {
        id: "msn-01",
        status: "WAITING",
        reason: "unforeseen-human-gate",
        result_code: null,
        cycle: 4,
        net_mrr_usd: 42.5,
        no_progress_cycles: 0,
        confirming: false,
        profile: "personal-autonomous-venture",
        target_mrr: 100,
        workflow_id: "wf-3bf104",
        title: "Reach $100 verified net MRR",
      },
      {
        id: "msn-02",
        status: "WAITING",
        reason: "budget",
        result_code: null,
        cycle: 7,
        net_mrr_usd: 88.0,
        no_progress_cycles: 1,
        confirming: true,
        profile: "personal-autonomous-venture",
        target_mrr: 100,
        workflow_id: "wf-9e11aa",
        title: "Near-target · budget pause",
      },
      {
        id: "msn-03",
        status: "CANCELLED",
        reason: null,
        result_code: "MISSION_TERMINATED_BY_POLICY",
        cycle: 2,
        net_mrr_usd: 0,
        no_progress_cycles: 2,
        confirming: false,
        profile: "personal-autonomous-venture",
        target_mrr: 100,
        workflow_id: "wf-kill09",
        title: "Terminated · prohibited market",
      },
      {
        id: "msn-10x-01",
        status: "RUNNING",
        reason: null,
        result_code: null,
        cycle: 3,
        net_mrr_usd: null,
        no_progress_cycles: 0,
        confirming: false,
        profile: "organization-10x",
        target_mrr: null,
        workflow_id: "wf-1120ab",
        title: "10x shared branch · payment service",
      },
      {
        id: "msn-10x-02",
        status: "WAITING",
        reason: "security-hold",
        result_code: null,
        cycle: 1,
        net_mrr_usd: null,
        no_progress_cycles: 0,
        confirming: false,
        profile: "organization-10x",
        target_mrr: null,
        workflow_id: "wf-10x-sec",
        title: "Org policy review · settings module",
      },
    ],

    approvals: [
      {
        id: "apr-01",
        prio: "P0",
        kind: "human-approval",
        title: "Approve plan · checkout polish",
        detail: "plan-id plan-7a2c · Risk High · Rev R3",
        workflow_id: "wf-7a2c91",
        profile: "personal-autonomous-venture",
        high_risk: true,
      },
      {
        id: "apr-02",
        prio: "P0",
        kind: "unforeseen-human-gate",
        title: "Mission ceremony · unforeseen gate",
        detail: "msn-01 · reason unforeseen-human-gate",
        workflow_id: "wf-3bf104",
        profile: "personal-autonomous-venture",
        high_risk: true,
      },
      {
        id: "apr-03",
        prio: "P1",
        kind: "budget",
        title: "Budget raise request",
        detail: "msn-02 · monthly cap 85% · propose +$40",
        workflow_id: "wf-9e11aa",
        profile: "personal-autonomous-venture",
        high_risk: false,
      },
      {
        id: "apr-04",
        prio: "P1",
        kind: "config-proposal",
        title: "Config proposal · wave_concurrency",
        detail: "tunables.yaml · 2 → 3 · admission hint A2",
        workflow_id: "wf-0aa991",
        profile: "personal-autonomous-venture",
        high_risk: false,
      },
      {
        id: "apr-10x-01",
        prio: "P0",
        kind: "security-hold",
        title: "Security hold · org policy",
        detail: "msn-10x-02 · reason security-hold",
        workflow_id: "wf-10x-sec",
        profile: "organization-10x",
        high_risk: true,
      },
      {
        id: "apr-10x-02",
        prio: "P1",
        kind: "human-approval",
        title: "Approve 10x wave · evidence gate",
        detail: "plan-id plan-10x · Risk High · Rev R3",
        workflow_id: "wf-1120ab",
        profile: "organization-10x",
        high_risk: true,
      },
    ],

    loops: [
      "Portfolio",
      "Delivery",
      "Recovery",
      "Capacity",
      "Capability",
      "Learning",
      "Memory",
      "Security",
    ],

    deployment() {
      return (
        this.deployments.find((d) => d.id === this.activeProfile) ||
        this.deployments[0]
      );
    },

    workflowsInScope() {
      return this.workflows.filter((w) => w.profile === this.activeProfile);
    },

    missionsInScope() {
      return this.missions.filter((m) => m.profile === this.activeProfile);
    },

    approvalsInScope() {
      return this.approvals.filter((a) => a.profile === this.activeProfile);
    },

    activeMissionsCount() {
      return this.missionsInScope().filter(
        (m) =>
          m.status === "RUNNING" ||
          m.status === "WAITING" ||
          m.status === "PENDING",
      ).length;
    },

    switchDeployment(id) {
      if (!this.deployments.some((d) => d.id === id)) return;
      if (id === this.activeProfile) return;
      try {
        localStorage.setItem(STORAGE_KEY, id);
      } catch (_) {}
      this.toast(
        "Switching trust domain → separate foundryd process (RefuseMultiProfileSingleProcess).",
        "warn",
      );
      const url = new URL(location.href);
      url.searchParams.set("deployment", id);
      url.searchParams.delete("mission");
      setTimeout(() => {
        location.href = url.toString();
      }, 450);
    },

    toast(msg, kind) {
      let root = document.getElementById("toast-root");
      if (!root) {
        root = document.createElement("div");
        root.id = "toast-root";
        document.body.appendChild(root);
      }
      const el = document.createElement("div");
      el.className = "toast" + (kind ? " " + kind : "");
      el.setAttribute("role", "status");
      el.setAttribute("aria-live", "polite");
      el.textContent = msg;
      root.appendChild(el);
      setTimeout(() => {
        el.style.opacity = "0";
        el.style.transition = "opacity 200ms";
        setTimeout(() => el.remove(), 220);
      }, 3200);
    },

    qs(name) {
      return new URLSearchParams(location.search).get(name);
    },

    workflow(id) {
      return this.workflows.find((w) => w.id === id);
    },

    mission(id) {
      return this.missions.find((m) => m.id === id);
    },

    statusClass(s) {
      return "status status-" + s;
    },

    path(page) {
      if (!page || page === "index.html" || page === "") {
        return ROOT === "." ? "index.html" : ROOT + "/index.html";
      }
      if (ROOT === ".") {
        return page.startsWith("pages/") ? page : "pages/" + page;
      }
      if (page.startsWith("pages/")) {
        return ROOT + "/" + page;
      }
      return ROOT + "/pages/" + page;
    },

    withDeployment(href) {
      try {
        const u = new URL(href, location.href);
        u.searchParams.set("deployment", this.activeProfile);
        const here = new URL(location.href);
        let rel = u.pathname;
        const baseDir = here.pathname.replace(/[^/]+$/, "");
        if (rel.startsWith(baseDir)) {
          rel = rel.slice(baseDir.length);
        } else {
          /* climb from pages/ to parent */
          const hereParts = here.pathname.split("/").filter(Boolean);
          const thereParts = u.pathname.split("/").filter(Boolean);
          let i = 0;
          while (
            i < hereParts.length - 1 &&
            i < thereParts.length &&
            hereParts[i] === thereParts[i]
          )
            i++;
          const up = hereParts.length - 1 - i;
          rel =
            (up > 0 ? "../".repeat(up) : "") + thereParts.slice(i).join("/");
        }
        return rel + u.search + (u.hash || "");
      } catch (_) {
        const join = href.indexOf("?") >= 0 ? "&" : "?";
        return (
          href + join + "deployment=" + encodeURIComponent(this.activeProfile)
        );
      }
    },

    ensurePackagingNav() {
      const href = this.path("packaging.html");
      const page = document.body.dataset.page || "";
      const packagingPages = [
        "packaging",
        "packaging-catalog",
        "packaging-enable",
        "packaging-install",
        "packaging-evolve",
      ];
      const isActive = packagingPages.indexOf(page) >= 0;

      document.querySelectorAll(".sidebar").forEach((side) => {
        if (side.querySelector('[data-nav="packaging"]')) return;
        const label = document.createElement("div");
        label.className = "nav-label";
        label.textContent = "Product packages";
        const link = document.createElement("a");
        link.className = "nav-item" + (isActive ? " active" : "");
        link.setAttribute("data-nav", "packaging");
        link.href = href;
        link.textContent = "Packaging";
        const foot = side.querySelector(".nav-foot");
        if (foot) {
          side.insertBefore(label, foot);
          side.insertBefore(link, foot);
        } else {
          side.appendChild(label);
          side.appendChild(link);
        }
      });

      document.querySelectorAll(".mobile-nav").forEach((nav) => {
        if (nav.querySelector('[data-nav="packaging"]')) return;
        const link = document.createElement("a");
        link.setAttribute("data-nav", "packaging");
        link.href = href;
        link.textContent = "Packaging";
        if (isActive) link.classList.add("active");
        nav.appendChild(link);
      });
    },

    ensureConfigSubnav() {
      const page = document.body.dataset.page || "";
      const configPages = [
        "config",
        "config-quotas",
        "config-rates",
        "config-mission-opp",
      ];
      if (configPages.indexOf(page) < 0) return;
      const main = document.querySelector("main.main");
      if (!main || main.querySelector(".config-subnav")) return;
      const head = main.querySelector(".page-head");
      const nav = document.createElement("nav");
      nav.className = "config-subnav pack-subnav";
      nav.setAttribute("aria-label", "Config sections");
      const items = [
        { page: "config", href: "config.html", label: "Layers" },
        { page: "config-quotas", href: "config-quotas.html", label: "Quotas" },
        {
          page: "config-rates",
          href: "config-rates.html",
          label: "Rates · Models",
        },
        {
          page: "config-mission-opp",
          href: "config-mission-opp.html",
          label: "Mission · Opp",
        },
      ];
      items.forEach((it) => {
        const a = document.createElement("a");
        a.href = this.path(it.href);
        a.textContent = it.label;
        if (it.page === page) a.classList.add("active");
        nav.appendChild(a);
      });
      if (head && head.nextSibling) {
        main.insertBefore(nav, head.nextSibling);
      } else if (head) {
        head.after(nav);
      } else {
        main.insertBefore(nav, main.firstChild);
      }
    },

    proposeTableChange(kind) {
      this.toast(
        (kind || "Config") +
          " change proposed → Approvals (mock). No live DB write.",
        "warn",
      );
      setTimeout(() => {
        location.href = this.withDeployment(this.path("approvals.html"));
      }, 900);
    },

    mountChrome() {
      if (!document.querySelector(".skip-link")) {
        const skip = document.createElement("a");
        skip.className = "skip-link";
        skip.href = "#main-content";
        skip.textContent = "Skip to main content";
        document.body.insertBefore(skip, document.body.firstChild);
      }
      const main = document.querySelector("main.main");
      if (main && !main.id) main.id = "main-content";
      main && main.setAttribute("tabindex", "-1");

      const dep = this.deployment();
      const meta = document.querySelector(".topbar-meta");
      if (meta) {
        meta.innerHTML =
          '<div class="trust-domain" role="group" aria-label="Trust domain">' +
          '<span class="trust-label">This process</span>' +
          '<select id="deployment-switch" class="deployment-switch" aria-label="Switch foundryd deployment">' +
          this.deployments
            .map(
              (d) =>
                '<option value="' +
                d.id +
                '"' +
                (d.id === this.activeProfile ? " selected" : "") +
                ">" +
                d.short +
                " · " +
                d.id +
                "</option>",
            )
            .join("") +
          "</select>" +
          '<span class="trust-api" title="API bound to this foundryd">' +
          dep.api +
          "</span>" +
          '<span class="trust-consistency">consistency <code>projected</code></span>' +
          "</div>";
        const sel = document.getElementById("deployment-switch");
        if (sel) {
          sel.addEventListener("change", () => {
            this.switchDeployment(sel.value);
          });
        }
      }

      const n = this.approvalsInScope().length;
      document
        .querySelectorAll(
          ".nav-item .count, .nav-item[data-nav='approvals'] .count",
        )
        .forEach((el) => {
          el.textContent = String(n);
          el.classList.toggle("alert", n > 0);
        });
      /* also mobile if plain text */
      document.querySelectorAll('a[data-nav="approvals"]').forEach((a) => {
        let c = a.querySelector(".count");
        if (!c && a.closest(".sidebar")) {
          /* already handled */
        }
      });

      document.querySelectorAll(".nav-foot").forEach((foot) => {
        foot.innerHTML =
          "Bound to <strong>" +
          this.activeProfile +
          "</strong>. One profile per foundryd. Other profiles = other process.";
      });

      this.ensurePackagingNav();
      this.ensureConfigSubnav();

      document.querySelectorAll("a[href]").forEach((a) => {
        const href = a.getAttribute("href");
        if (
          !href ||
          href.startsWith("#") ||
          href.startsWith("http") ||
          href.startsWith("mailto:")
        )
          return;
        if (href.includes("deployment=")) return;
        if (!/\.html/.test(href)) return;
        a.setAttribute("href", this.withDeployment(href));
      });
    },
  };

  document.querySelectorAll("[data-nav]").forEach((a) => {
    const key = a.getAttribute("data-nav");
    const page = document.body.dataset.page || "";
    if (key === page) a.classList.add("active");
    if (key === "config" && page.indexOf("config") === 0)
      a.classList.add("active");
  });

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", () =>
      FoundryMock.mountChrome(),
    );
  } else {
    FoundryMock.mountChrome();
  }

  document.addEventListener("click", (e) => {
    const btn = e.target.closest("[data-action]");
    if (!btn) return;
    const action = btn.getAttribute("data-action");
    if (action === "toast") {
      e.preventDefault();
      FoundryMock.toast(
        btn.getAttribute("data-msg") || "Done (mock)",
        btn.getAttribute("data-kind") || "ok",
      );
    }
    if (action === "propose-config") {
      e.preventDefault();
      FoundryMock.proposeTableChange(btn.getAttribute("data-kind") || "Config");
    }
    if (action === "propose-catalog") {
      e.preventDefault();
      FoundryMock.proposeTableChange("Catalog");
    }
    if (action === "propose-enable-toggles") {
      e.preventDefault();
      FoundryMock.proposeTableChange("Enablement");
    }
    if (action === "raise-budget") {
      e.preventDefault();
      FoundryMock.toast(
        "Budget raise request queued for human review (mock).",
        "warn",
      );
    }
    if (action === "verify-evidence") {
      e.preventDefault();
      const ok = btn.getAttribute("data-result") === "ok";
      FoundryMock.toast(
        ok ? "Evidence verify: OK (mock)" : "Evidence verify: REJECTED (mock)",
        ok ? "ok" : "warn",
      );
      const target = document.getElementById(btn.getAttribute("data-target"));
      if (target) {
        target.textContent = ok ? "verified" : "rejected";
        target.className = ok ? "verify-ok mono" : "verify-fail mono";
      }
    }
    if (action === "webauthn") {
      e.preventDefault();
      const box = document.getElementById("webauthn-box");
      const submit = document.getElementById("approve-submit");
      if (box) {
        box.classList.add("done");
        box.querySelector(".webauthn-status").textContent =
          "Credential asserted (mock)";
      }
      if (submit) submit.disabled = false;
      FoundryMock.toast("WebAuthn ceremony complete (mock)", "ok");
    }
    if (action === "approve-plan") {
      e.preventDefault();
      FoundryMock.toast(
        "Approval recorded via secure surface (mock). Telegram was not used.",
        "ok",
      );
      setTimeout(() => {
        location.href = FoundryMock.withDeployment(
          FoundryMock.path("approvals.html"),
        );
      }, 1000);
    }
    if (action === "reject-plan") {
      e.preventDefault();
      FoundryMock.toast("Rejection recorded (mock).", "warn");
      setTimeout(() => {
        location.href = FoundryMock.withDeployment(
          FoundryMock.path("approvals.html"),
        );
      }, 800);
    }
    if (action === "switch-deployment") {
      e.preventDefault();
      FoundryMock.switchDeployment(btn.getAttribute("data-deployment"));
    }
    if (action === "packaging-validate") {
      e.preventDefault();
      const r = FoundryMock.validatePackaging();
      FoundryMock.toast(r.message + " (mock)", r.ok ? "ok" : "warn");
      const el = document.getElementById("validate-result");
      if (el) {
        el.textContent = r.message;
        el.className = r.ok ? "verify-ok mono" : "verify-fail mono";
      }
    }
    if (action === "packaging-install") {
      e.preventDefault();
      const r = FoundryMock.validatePackaging();
      if (!r.ok) {
        FoundryMock.toast(
          "Install refused — validate failed (fail closed)",
          "warn",
        );
        return;
      }
      FoundryMock.toast(
        "Materialized enabled packages → claude-code workspace (mock). Allowlist unchanged.",
        "ok",
      );
      const st = document.getElementById("install-status");
      if (st) {
        st.textContent = "installed";
        st.className = "verify-ok mono";
      }
    }
    if (action === "packaging-doctor") {
      e.preventDefault();
      const en = FoundryMock.packagingForActive();
      const ok = en.install.doctor === "ok";
      FoundryMock.toast(
        ok
          ? "Doctor OK — pins + files present (mock)"
          : "Doctor WARN — " + (en.install.doctor_note || "mismatch"),
        ok ? "ok" : "warn",
      );
    }
    if (action === "packaging-promote") {
      e.preventDefault();
      if (FoundryMock.activeProfile === "organization-10x") {
        FoundryMock.toast(
          "Org path: proposal-only — no auto-activate (mock)",
          "warn",
        );
        return;
      }
      FoundryMock.toast(
        "L1 promote → on-disk version retained previous (mock)",
        "ok",
      );
    }
    if (action === "packaging-rollback") {
      e.preventDefault();
      FoundryMock.toast(
        "Rollback to previous skill version (mock). History retained.",
        "ok",
      );
    }
    if (action === "packaging-propose-enable") {
      e.preventDefault();
      FoundryMock.toast(
        "Enablement change proposed → Approvals (mock). No live YAML write.",
        "warn",
      );
      setTimeout(() => {
        location.href = FoundryMock.withDeployment(
          FoundryMock.path("approvals.html"),
        );
      }, 900);
    }
  });
})();
