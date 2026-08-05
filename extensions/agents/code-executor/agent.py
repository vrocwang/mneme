#!/usr/bin/env python3
"""Code Executor Agent extension for Mneme.

Provides a code execution agent that runs code in sandboxed
environments (shell, scripts, tests).

Protocol: stdin/stdout JSON-RPC 2.0 (one message per line).
"""
import json
import sys

MANIFEST = {
    "name": "code-executor",
    "version": "0.1.0",
    "description": "Code execution agent",
    "tools": [],
    "agent_defs": ["code_executor"],
    "protocol_min": 1,
}

AGENTS = [
    {
        "id": "code_executor",
        "name": "Code Executor",
        "description": "Executes code in sandboxed environments. Can run shell commands, scripts, tests, and build tools. Specialized in safe code execution and output analysis.",
        "tier": "worker",
        "systemPrompt": "You are a Code Executor agent specialized in running code safely. You execute shell commands, scripts, and tests, then analyze the output. Always consider security implications.",
        "toolAllowlist": ["read_file", "write_file", "list_dir", "shell", "glob", "grep", "run_tests", "run_linter", "current_time"],
        "maxIterations": 15,
        "hidden": False,
    }
]


def handle_request(req):
    method = req.get("method", "")
    req_id = req.get("id", 0)

    if method == "extension.describe":
        return {"jsonrpc": "2.0", "id": req_id, "result": MANIFEST}
    elif method == "extension.list_tools":
        return {"jsonrpc": "2.0", "id": req_id, "result": {"tools": []}}
    elif method == "extension.list_agents":
        return {"jsonrpc": "2.0", "id": req_id, "result": {"agents": AGENTS}}
    elif method == "extension.call_tool":
        return {"jsonrpc": "2.0", "id": req_id, "result": {"error": "no tools available"}}
    else:
        return {"jsonrpc": "2.0", "id": req_id, "error": {"code": -32601, "message": f"unknown method: {method}"}}


def main():
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
            resp = handle_request(req)
            sys.stdout.write(json.dumps(resp) + "\n")
            sys.stdout.flush()
        except json.JSONDecodeError:
            pass


if __name__ == "__main__":
    main()
