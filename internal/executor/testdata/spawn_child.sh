#!/bin/sh
# Backgrounds a long-running grandchild, records its pid to $1, then blocks
# in the foreground so the direct child (this script) stays alive until its
# process group is killed. Used by subprocess_test.go to prove that killing
# the process group also kills processes it spawned, not just itself.
sleep 100 &
echo $! > "$1"
wait
