#!/bin/sh
export LD_LIBRARY_PATH=$LAMBDA_TASK_ROOT
exec $LAMBDA_TASK_ROOT/optipng "$@"
