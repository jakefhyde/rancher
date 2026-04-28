// Package render implements the Go-template rendering engine that turns a
// ClusterPlanTemplate body into a ClusterPlanSpec, and a NodePlanSpec body
// into a fully-rendered NodePlanSpec ready for system-agent execution.
//
// Two render contexts exist deliberately:
//
//  1. RenderClusterPlan(...) renders the *outer* template body once at Beacon
//     acquisition. It has access to cluster-side objects (Entrypoint, owner
//     chains, secrets, conditions) but not to per-stage outputs (none have
//     been captured yet). Its result is parsed as YAML into ClusterPlanSpec.
//
//  2. RenderNodePlanSpec(...) re-renders the templated string fields of an
//     already-structured NodePlanSpec at the time the framework is about to
//     emit a NodePlan to a single node. It additionally has access to the
//     accumulated outputs of prior stages via output / shard / getCapture.
//
// A small role-DSL pre-processor (preprocess.go) rewrites
// `{{ if etcd & !controlplane }} ... {{ fi }}` shorthand into legal
// text/template syntax. Authors may use either the sugar or stdlib syntax;
// the sugar only triggers on expressions whose body is purely DSL tokens
// (identifiers, &, |, !, parentheses, whitespace).
//
// All k8s reads from inside templates flow through RenderClient, which is
// expected to enforce a GVK + namespace + name allow-list; the engine itself
// is sandboxed and never writes.
package render
