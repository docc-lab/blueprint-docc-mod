# Return-path (reverse) context propagation — design note

**Date:** 2026-07-20
**Status:** DESIGN / first-principles discussion (no experiments yet)
**Scope:** research framing for a return-direction propagation mechanism in the
span-based (Dapper-style) tracing model, and where it lands in Blueprint's
instrumentation (`ir_ot_client.go` / `ir_ot_server.go`).

This note captures the *beginning* of the discussion — the first-principles
framing we converged on plus the prior-art sources uncovered so far. It is a
living design note, not a settled position.

---

## 1. The problem, from first principles

Strip a Dapper-style span system to primitives: every hop is a **request →
response** pair. Context propagation today only ever rides the **request** half —
information flows caller→callee, ancestor→descendant, cause→effect (trace ID,
sampling decision, deadlines, W3C baggage, config). The **response** half carries
the application result and, tracing-wise, *nothing*. It is a dead channel.

So the core observation is not "add a lever to accumulation." It is:

> **The propagation model is asymmetric. We are completing the symmetry.**

- **Forward** = ancestor → descendant. Carries *intent*.
- **Return** = descendant → ancestor. Carries *evidence / results* (what actually
  happened in the subtree).

"Accumulation" is not the essence of the forward path — it is just one thing you
can *do* on it. The essence is **direction**. Once direction is named as the
primary axis, merge-policy and reset-vs-accumulate fall out as **sub-levers that
exist on both directions**.

### 1a. Why the return direction is not merely "forward, backwards"

The structural asymmetry that makes this a distinct mechanism:

> **Forward propagation fans OUT. Return propagation fans IN.**

A parent handing context to N children is a trivial *copy* — no combination
needed. That is why forward baggage rarely needs a nontrivial merge; the sole
exception (concurrent branches rejoining) is exactly the narrow case the Tracing
Plane / Baggage Buffers merge operator was built for.

A parent *receiving* context from N children is **constitutively** a combination,
and it happens at **every internal node** of the call tree — not as an exception.
So return-path propagation does not merely "add a merge someday at a fan-in"; it
makes the **merge algebra the central object**. That is the structural reason this
is a different beast and the strongest novelty hook:

> **Applying merge algebra to the return direction of synchronous RPC, where
> fan-in is the common case rather than the edge case.**

### 1b. The "third plane" argument (bridges connection)

Standard tracing has two planes that never meet in-band:

1. **Forward in-band plane** — intent, synchronous, downward.
2. **Offline global plane** — the collector reconstructs the whole tree,
   asynchronously, *after the fact*.

"What happened in my subtree" is knowable today only on plane 2 (after export +
reconstruction). Return-path propagation is a **third plane: return in-band** —
subtree evidence, synchronously, upward. It partially *subsumes* what the
collector does, but online and locally.

Punchline for the bridges paper: **the bridges evidence structures are already
merge algebras — currently run forward and merged offline.** A PB bloom is a
union of span IDs; a parent's bloom = union(children's blooms) ∪ own ID = a
semilattice join. Return-path propagation is the **same fold run bottom-up
in-band**. This is not a bolt-on direction — it is a second implementation point
of bridges' own reconstruction, differing only in **where and when the merge
happens** (offline-at-collector vs online-at-caller). That makes the return-path
work feel inevitable rather than tacked on.

---

## 2. The merge algebra → swappable FP categories

Cast `mergeReturn` as the combine operator; its algebraic properties decide what
is safe and whether the accumulate scope is affordable:

| category | properties | example | bounded under accumulate-to-root? |
|---|---|---|---|
| **semilattice / set-join** | idempotent + commutative + assoc | bloom union ("which services did my subtree touch") | **yes** (fixed width) |
| **monoid / reducer** | commutative + assoc | Σ cost, max depth, span count | yes (scalar) |
| **list / concat** | assoc, *not* commutative | ordered event log | **no** (O(subtree)) |
| **last-write** | *not* commutative | most-recent health signal | bounded but order-dependent |

**Key result linking the two levers:** the merge algebra decides whether
*accumulate* is even affordable. An idempotent semilattice (bloom) stays
fixed-width regardless of fold depth → accumulate-to-root is free. A concat merge
blows up O(subtree) → it *forces* reset-at-hop. So reset-vs-accumulate is **not**
an independent free choice — it is constrained by the algebra.

**Dual hot-spot geometry:** forward accumulation blows up by fanning *out* (every
leaf carries the root's baggage); return accumulation blows up by *converging*
(the root becomes an O(tree) hot point). Same blowup, mirrored geometry.

---

## 3. Open forks (unresolved)

1. **What is return-path propagation *for* in the paper?** Candidates:
   (a) in-band loss-tolerant reconstruction, (b) reactive control (callee returns
   load/cost → caller sheds/hedges), (c) cost attribution (subtree cost bubbles
   up, killing offline critical-path analysis), (d) provenance/attestation.
   Current lean: **(a)** — same fold as bridges → tightest story. Undecided
   whether to frame as "bridges, but in-band" or as a general mechanism with
   reconstruction as one instance.

2. **Synchrony at fan-in.** A caller with N children gets N return contexts at
   different times. Commutative monoid → fold online, can act on a *partial* merge
   (after k of N). Non-commutative → must buffer and order. Open: keep "act on
   partial subtree evidence before all children return" as a lever, or require
   strictly complete-subtree return context (emit only after last child)?

---

## 4. Blueprint anchor

Implementation point identified: a `retCtx` return value threaded alongside the
existing forward `ctx`/baggage in the generated wrappers —
`plugins/opentelemetry/ir_ot_client.go` (caller side: receive + merge children's
retCtx) and `ir_ot_server.go` (callee side: produce retCtx on the response path).
`mergeReturn` is the swappable policy from §2. Not yet implemented.

---

## 5. Prior art / sources uncovered

Grouped by role in the argument. URLs captured from the earlier targeted search;
where a URL is a landing page rather than the PDF it is noted.

### Merge algebra at fan-in (the algebra is prior art — the *direction* is not)

- **The Tracing Plane / "Universal Context Propagation for Distributed System
  Instrumentation"** — Jonathan Mace, Rodrigo Fonseca, EuroSys 2018. Formalizes
  baggage as a data type (Baggage Buffers / BDL) with Branch / **Merge (Join)** /
  Trim / Serialize / Deserialize ops; the merge fires when concurrent *forward*
  branches rejoin. This is the closest prior art for the merge operator.
  - ACM DL: https://dl.acm.org/doi/10.1145/3190508.3190526
  - Semantic Scholar: https://www.semanticscholar.org/paper/Universal-context-propagation-for-distributed-Mace-Fonseca/c5711106240e93aeed810db7bc789d8d055e9de3
  - Implementation: https://github.com/tracingplane/tracingplane-java
  - **Open item (deferred):** fetch the Baggage Buffers / BDL paper + Mace's
    thesis to settle whether the explicit **CRDT / join-semilattice / lattice**
    framing of the merge is theirs or a contribution we can claim. The mechanism
    (merge-at-fan-in) is clearly theirs; the *algebraic/CRDT vocabulary* may be
    partly ours to add. Not yet confirmed.

- **Pivot Tracing** — Jonathan Mace, Ryan Roelke, Rodrigo Fonseca, SOSP 2015.
  Happened-before join + in-band aggregation of baggage, but aggregation is
  consumed at the query/collector, **not handed back to the caller in-band** on
  the response path. (Verify exact URL before citing — ACM DL doi
  10.1145/2815400.2815415 for SOSP'15.)

### The span-based model we scope to (and why we exclude general DAGs)

- **Dapper** — Sigelman et al., Google tech report, 2010. The span/parent-child
  call-tree model + strict request-response pairing that gives return propagation
  a natural carrier (piggyback the response).
  - https://research.google.com/archive/papers/dapper-2010-1.pdf
  - Mirror: https://static.googleusercontent.com/media/research.google.com/en//archive/papers/dapper-2010-1.pdf

- **X-Trace** — Fonseca, Porter, Katz, Shenker, Stoica, NSDI 2007 (+ "Experiences
  with X-Trace", Fonseca/Freedman/Porter 2010). General **DAG** with explicit join
  edges; *can* represent joins and richer causality, but it is forward
  edge-construction and does **not** hand aggregated subtree state back up the call
  stack during the response. This is precisely why we scope to the span/Dapper
  model — a general async DAG lacks the clean response carrier. (Confirm canonical
  USENIX NSDI'07 PDF URL before citing.)

- **Canopy** — Kaldor et al., Facebook, SOSP 2017. Production span-based
  end-to-end tracing/analysis; reconstruction + modeling happen offline (plane 2).
  - ACM DL: https://dl.acm.org/doi/10.1145/3132747.3132749
  - The Morning Paper summary: https://blog.acolyer.org/2017/11/22/canopy-an-end-to-end-performance-tracing-and-analysis-system/

- **"Principled Workflow-Centric Tracing of Distributed Systems"** — Mace,
  Fonseca, Ganger (2016, SoCC). Design-space framing of tracing; relevant to how
  causal metadata is structured. (Cited in the eval-structure thread; verify DOI.)

### The forward-baggage standard (the thing we are the dual of)

- **W3C Baggage** — the standardized forward baggage propagation format.
  - https://github.com/w3c/baggage/

### Author hubs (for chasing the lineage)

- Jonathan Mace: https://jonathanmace.github.io/ and https://cs.brown.edu/people/jcmace/

### Positioning summary

- **Merge-at-fan-in = prior art** (Tracing Plane / Pivot Tracing lineage) → cite,
  do not claim.
- **Merge on the return-direction of synchronous RPC = open ground.** We inherit
  their algebra and apply it to a channel these systems do not touch.
- **Possibly ours:** the explicit CRDT/semilattice/monoid *formalization* of the
  merge policy as swappable FP categories — pending a close read of Baggage
  Buffers to confirm they did not already state it in those terms.

---

## 6. Next actions (not yet authorized)

- (deferred) Fetch Baggage Buffers / BDL paper to settle the CRDT-framing question.
- (deferred) Confirm exact citable URLs for X-Trace NSDI'07 and Pivot Tracing SOSP'15.
- Decide fork §3.1 (the "what for") before any implementation of `retCtx`.
