// Package adapter abstracts the per-cluster-type concerns of the day-2
// ops framework so the controllers in pkg/controllers/plan/... can
// operate uniformly across CAPR (v2prov), CAPRKE2 (upstream), and
// imported RKE2/K3s clusters.
//
// Each adapter answers four classes of question:
//
//   1. Cluster shape — what is the entrypoint object, what nodes belong
//      to the cluster, what are their owner chains?
//   2. Plan delivery — write a rendered wire-format NodePlan to a
//      transport the system-agent on that node will read.
//   3. Plan status — read the agent's reply back into a typed
//      NodePlanStatus.
//   4. Per-node lifecycle — apply / remove an election label, report
//      whether the agent is ready to receive plans at all.
//
// The interface is intentionally narrow so each implementation can stay
// small. Production wire-up (registering adapters with their wrangler
// caches and clustermanager) lives in pkg/controllers/plan/plan.go.
//
// Wire format: every adapter today writes the existing system-agent
// machine-plan secret format via pkg/clusterplan/wire. PR6 retires the
// wire package and the adapters' WriteNodePlan implementations
// reduce to "publish the NodePlan + ensure the agent can authenticate
// to read it" — the per-cluster transport differences disappear.
package adapter
