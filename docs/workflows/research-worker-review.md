# Research → Worker → Review

This is how we want agents to ship non-trivial work on this project (and how you should think about agentic coding long-term).

## Why three lanes?

One model doing research + coding + self-review in a single pass tends to:

1. **Skip evidence** — invent APIs that “should” exist  
2. **Over-scope** — refactor while “just adding Swagger”  
3. **Self-approve** — miss the bug it just introduced  

Separating roles fixes that the same way engineering teams do:

| Lane | Job | Mindset |
|------|-----|---------|
| **Research** | Map reality, choose an approach | Curious, read-only, cite files |
| **Worker** | Make the smallest correct change | Obedient to the plan, verify with tests |
| **Review** | Try to break it / find gaps | Skeptical, high bar, no rubber stamp |

You (human) + the orchestrator stay in charge: lanes report; they don’t silently redefine the product.

## What “good” looks like

```text
You: “Add X with the same flow.”

Orchestrator:
  1. Spawns Research → returns plan + pitfalls + acceptance checks
  2. Spawns Worker with that plan → commits + test evidence
  3. Spawns Review on those commits → only real issues
  4. Fixes Critical/Important if any; ships with a short summary
```

## Teaching notes (for you)

- **Research quality** determines worker quality. Bad research → wrong abstraction → expensive rework.  
- **Worker** should be boring: follow the plan, small commits, run tests. Cleverness belongs in research trade-offs.  
- **Review** is not “say something nice.” Ask: *What would fail at 2am?* For this service: duplicate POST, double-ack, lost jobs, hammering GitHub after 429.  
- **When to skip lanes:** typo fixes, one-line renames, pure questions. Not for queues, caches, or anything in `AGENTS.md` invariants.

## Where this is encoded

| Artifact | Role |
|----------|------|
| `~/.cursor/skills/research-worker-review/SKILL.md` | Personal skill (all projects) |
| `.cursor/skills/research-worker-review/SKILL.md` | Project skill (this repo); Research starts from the design spec |
| `.cursor/skills/micro-rwr/SKILL.md` | Compressed live-change loop: plan → test → implement → invariant checklist |
| `.github/pull_request_template.md` | PR checklist mirroring `AGENTS.md` Validate section |
| `AGENTS.md` | Short pointer + repo invariants |
| This file | Human teaching / onboarding |

## Micro-RWR (live / interview)

Same roles, shorter ceremony. Use when blast radius is small but invariants still matter:

1. Read design + name the invariant at risk  
2. Write/extend a focused test  
3. Implement the smallest diff  
4. Tick the applicable validate checklist  

Full three-lane still required for topology, new endpoints, or anything you would not want to demo unreviewed.

## Trigger phrases

Agents should apply **full RWR** when you say things like:

- “same flow”  
- “research, worker, review”  
- “three-lane” / “agentic best practice”

Agents should apply **micro-RWR** when you say:

- “micro-RWR” / “live change” / “interview change”
