#!/bin/sh
# OpenSandbox sources this before starting its daemon; replace bootstrap PID 1.
exec /usr/local/bin/execution-sandbox -mode network-init
