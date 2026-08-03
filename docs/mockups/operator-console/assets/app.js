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

  window.FoundryMock = {
    deployments: DEPLOYMENTS,
    activeProfile: resolveActiveProfile(),

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
      return this.deployments.find((d) => d.id === this.activeProfile) || this.deployments[0];
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
      return this.missionsInScope().filter((m) => m.status === "RUNNING" || m.status === "WAITING" || m.status === "PENDING")
        .length;
    },

    switchDeployment(id) {
      if (!this.deployments.some((d) => d.id === id)) return;
      if (id === this.activeProfile) return;
      try {
        localStorage.setItem(STORAGE_KEY, id);
      } catch (_) {}
      this.toast(
        "Switching trust domain → separate foundryd process (RefuseMultiProfileSingleProcess).",
        "warn"
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
          while (i < hereParts.length - 1 && i < thereParts.length && hereParts[i] === thereParts[i]) i++;
          const up = hereParts.length - 1 - i;
          rel = (up > 0 ? "../".repeat(up) : "") + thereParts.slice(i).join("/");
        }
        return rel + u.search + (u.hash || "");
      } catch (_) {
        const join = href.indexOf("?") >= 0 ? "&" : "?";
        return href + join + "deployment=" + encodeURIComponent(this.activeProfile);
      }
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
                "</option>"
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
      document.querySelectorAll(".nav-item .count, .nav-item[data-nav='approvals'] .count").forEach((el) => {
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

      document.querySelectorAll("a[href]").forEach((a) => {
        const href = a.getAttribute("href");
        if (!href || href.startsWith("#") || href.startsWith("http") || href.startsWith("mailto:")) return;
        if (href.includes("deployment=")) return;
        if (!/\.html/.test(href)) return;
        a.setAttribute("href", this.withDeployment(href));
      });
    },
  };

  document.querySelectorAll("[data-nav]").forEach((a) => {
    const key = a.getAttribute("data-nav");
    const page = document.body.dataset.page;
    if (key === page) a.classList.add("active");
  });

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", () => FoundryMock.mountChrome());
  } else {
    FoundryMock.mountChrome();
  }

  document.addEventListener("click", (e) => {
    const btn = e.target.closest("[data-action]");
    if (!btn) return;
    const action = btn.getAttribute("data-action");
    if (action === "toast") {
      e.preventDefault();
      FoundryMock.toast(btn.getAttribute("data-msg") || "Done (mock)", btn.getAttribute("data-kind") || "ok");
    }
    if (action === "propose-config") {
      e.preventDefault();
      FoundryMock.toast("Proposal submitted → Approvals (mock). Kernel not contacted.", "warn");
      setTimeout(() => {
        location.href = FoundryMock.withDeployment(FoundryMock.path("approvals.html"));
      }, 900);
    }
    if (action === "raise-budget") {
      e.preventDefault();
      FoundryMock.toast("Budget raise request queued for human review (mock).", "warn");
    }
    if (action === "verify-evidence") {
      e.preventDefault();
      const ok = btn.getAttribute("data-result") === "ok";
      FoundryMock.toast(
        ok ? "Evidence verify: OK (mock)" : "Evidence verify: REJECTED (mock)",
        ok ? "ok" : "warn"
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
        box.querySelector(".webauthn-status").textContent = "Credential asserted (mock)";
      }
      if (submit) submit.disabled = false;
      FoundryMock.toast("WebAuthn ceremony complete (mock)", "ok");
    }
    if (action === "approve-plan") {
      e.preventDefault();
      FoundryMock.toast("Approval recorded via secure surface (mock). Telegram was not used.", "ok");
      setTimeout(() => {
        location.href = FoundryMock.withDeployment(FoundryMock.path("approvals.html"));
      }, 1000);
    }
    if (action === "reject-plan") {
      e.preventDefault();
      FoundryMock.toast("Rejection recorded (mock).", "warn");
      setTimeout(() => {
        location.href = FoundryMock.withDeployment(FoundryMock.path("approvals.html"));
      }, 800);
    }
    if (action === "switch-deployment") {
      e.preventDefault();
      FoundryMock.switchDeployment(btn.getAttribute("data-deployment"));
    }
  });
})();
