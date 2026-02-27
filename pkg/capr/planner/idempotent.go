package planner

import (
	"fmt"
	"strings"

	"github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1/plan"
)

// idempotentActionScript wraps a provided command in additional checks which ensure the command
// will be attempted until it is successfully run once, and then prevent it from being
// run again - effectively making arbitrary commands idempotent. This prevents
// potential re-execution of commands which must only be run once (etcd operations, etc.)
// but are not idempotent by default. The command will be reattempted until either it is
// successful, or the max-failures limit set for the plan is reached. The definition of
// $CATTLE_AGENT_ATTEMPT_NUMBER can be found in the system-agent repository, but it is effectively
// just the plans failure-count + 1.
const idempotentActionScript = `
#!/bin/sh

currentHash=""
key=$1
targetHash=$2
hashedCmd=$3
cmd=$4
caprDir=$5
shift 5

dataRoot="$caprDir/idempotence/$key/$hashedCmd/$targetHash"
attemptFile="$dataRoot/last-attempt"

currentAttempt=$(cat "$attemptFile" || echo "-1")

if [ "$currentAttempt" != "$CATTLE_AGENT_ATTEMPT_NUMBER" ]; then
	mkdir -p "$dataRoot"
	echo "$CATTLE_AGENT_ATTEMPT_NUMBER" > "$attemptFile"
	exec "$cmd" "$@"
else
	echo "action has already been reconciled to the target hash $targetHash at attempt $currentAttempt"
fi
`

// todo(jhyde): make idempotency first class
func idempotentActionScriptPath() string {
	return "/var/lib/rancher/capr/idempotence/idempotent.sh"
}

// generateIdempotencyCleanupInstruction generates a one-time instruction that performs a cleanup of the given key.
// todo(jhyde): make idempotency first class
func generateIdempotencyCleanupInstruction(key string) plan.OneTimeInstruction {
	if key == "" {
		return plan.OneTimeInstruction{}
	}
	return plan.OneTimeInstruction{
		Name:    "remove idempotency tracking",
		Command: "/bin/sh",
		Args: []string{
			"-c",
			fmt.Sprintf("rm -rf %s/idempotence/%s", "/var/lib/rancher/capr", key),
		},
	}
}

// idempotentInstruction generates an idempotent action instruction that will execute the given command + args exactly once.
// It works by running a script that writes the given "value" to a file at /var/lib/rancher/capr/idempotence/<identifier>/<hashedCommand>,
// and checks this file to determine if it needs to run the instruction again. Notably, `identifier` must be a valid relative path.
// todo(jhyde): make idempotency first class
func idempotentInstruction(identifier, value, command string, args []string, env []string) plan.OneTimeInstruction {
	hashedCommand := PlanHash([]byte(command))
	hashedValue := PlanHash([]byte(value))
	return plan.OneTimeInstruction{
		Name:    fmt.Sprintf("idempotent-%s-%s-%s", identifier, hashedValue, hashedCommand),
		Command: "/bin/sh",
		Args: append([]string{
			"-x",
			idempotentActionScriptPath(),
			strings.ToLower(identifier),
			hashedValue,
			hashedCommand,
			command,
			"/var/lib/rancher/capr"},
			args...),
		Env: env,
	}
}

// convertToIdempotentInstruction converts a OneTimeInstruction to a OneTimeInstruction wrapped with the idempotent script.
// This is useful when an instruction may be used in various phases, without needing idempotency in all cases.
// identifier is expected to be a unique key for tracking, and value should be something like the generation of the attempt
// (and is what we track to determine whether we should run the instruction or not)
func convertToIdempotentInstruction(identifier, value string, instruction plan.OneTimeInstruction) plan.OneTimeInstruction {
	newInstruction := idempotentInstruction(identifier, value, instruction.Command, instruction.Args, instruction.Env)
	newInstruction.Image = instruction.Image
	newInstruction.SaveOutput = instruction.SaveOutput
	return newInstruction
}

// idempotentRestartInstructions generates an idempotent restart instructions for the given runtimeUnit. It checks the
// unit for failure, resets it if necessary, and restarts the unit. identifier is expected to be a unique key for tracking,
// and value should be something like the generation of the attempt (and is what we track to determine whether we should run the instruction or not)
func idempotentRestartInstructions(identifier, value, runtimeUnit string) []plan.OneTimeInstruction {
	return []plan.OneTimeInstruction{
		idempotentInstruction(
			identifier+"-reset-failed",
			value,
			"/bin/sh",
			[]string{
				"-c",
				fmt.Sprintf("if [ $(systemctl is-failed %s) = failed ]; then systemctl reset-failed %s; fi", runtimeUnit, runtimeUnit),
			},
			[]string{},
		),
		idempotentInstruction(
			identifier+"-restart",
			value,
			"systemctl",
			[]string{
				"restart",
				runtimeUnit,
			},
			[]string{},
		),
	}
}

// idempotentStopInstruction generates an idempotent stop instruction for the given runtimeUnit. It simply calls systemctl stop <runtime-unit>
// identifier is expected to be a unique key for tracking, and value should be something like the generation of the attempt (and is what we track to determine whether we should run the instruction or not)
func idempotentStopInstruction(identifier, value, runtimeUnit string) plan.OneTimeInstruction {
	return idempotentInstruction(
		identifier+"-stop",
		value,
		"systemctl",
		[]string{
			"stop",
			runtimeUnit,
		},
		[]string{},
	)
}
