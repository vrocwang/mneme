#!/usr/bin/env python3
"""Thread Goals extension for Mneme.

Provides tools for tracking and managing conversation goals per thread.
Goals are persisted to a JSON file in the workspace data directory.

Protocol: stdin/stdout JSON-RPC 2.0 (one message per line).
"""
import json
import os
import sys
import time

MANIFEST = {
    "name": "thread-goals",
    "version": "0.1.0",
    "description": "Thread goal tracking tools",
    "tools": [
        {
            "name": "set_thread_goal",
            "description": "Set or update the goal for a conversation thread. The goal guides the agent's focus and task completion criteria.",
            "parameters": {
                "type": "object",
                "properties": {
                    "thread_id": {"type": "string", "description": "Thread ID"},
                    "goal": {"type": "string", "description": "Goal description"},
                    "criteria": {"type": "array", "items": {"type": "string"}, "description": "Optional completion criteria"},
                },
                "required": ["thread_id", "goal"],
            },
        },
        {
            "name": "get_thread_goal",
            "description": "Retrieve the current goal for a conversation thread.",
            "parameters": {
                "type": "object",
                "properties": {
                    "thread_id": {"type": "string", "description": "Thread ID"},
                },
                "required": ["thread_id"],
            },
        },
        {
            "name": "complete_thread_goal",
            "description": "Mark a thread goal as completed.",
            "parameters": {
                "type": "object",
                "properties": {
                    "thread_id": {"type": "string", "description": "Thread ID"},
                    "summary": {"type": "string", "description": "Optional completion summary"},
                },
                "required": ["thread_id"],
            },
        },
        {
            "name": "list_thread_goals",
            "description": "List all active (non-completed) thread goals.",
            "parameters": {
                "type": "object",
                "properties": {},
            },
        },
    ],
    "agent_defs": [],
    "protocol_min": 1,
}

GOALS_FILE = os.path.join(
    os.environ.get("MNEME_HOME", os.path.expanduser("~/.mneme")),
    "data",
    "thread_goals.json",
)


def load_goals():
    """Load goals from the JSON file."""
    try:
        with open(GOALS_FILE, "r") as f:
            return json.load(f)
    except (FileNotFoundError, json.JSONDecodeError):
        return {}


def save_goals(goals):
    """Save goals to the JSON file."""
    os.makedirs(os.path.dirname(GOALS_FILE), exist_ok=True)
    with open(GOALS_FILE, "w") as f:
        json.dump(goals, f, indent=2)


def set_thread_goal(thread_id, goal, criteria=None):
    goals = load_goals()
    goals[thread_id] = {
        "goal": goal,
        "criteria": criteria or [],
        "status": "active",
        "created_at": goals.get(thread_id, {}).get("created_at", time.time()),
        "updated_at": time.time(),
    }
    save_goals(goals)
    return {"success": True, "thread_id": thread_id, "goal": goal}


def get_thread_goal(thread_id):
    goals = load_goals()
    if thread_id in goals:
        return goals[thread_id]
    return {"success": True, "thread_id": thread_id, "goal": None, "message": "no goal set"}


def complete_thread_goal(thread_id, summary=""):
    goals = load_goals()
    if thread_id not in goals:
        return {"error": f"no goal found for thread {thread_id}"}
    goals[thread_id]["status"] = "completed"
    goals[thread_id]["completed_at"] = time.time()
    goals[thread_id]["summary"] = summary
    save_goals(goals)
    return {"success": True, "thread_id": thread_id, "status": "completed"}


def list_thread_goals():
    goals = load_goals()
    active = {k: v for k, v in goals.items() if v.get("status") == "active"}
    return {"success": True, "goals": active, "count": len(active)}


def handle_request(req):
    method = req.get("method", "")
    req_id = req.get("id", 0)

    if method == "extension.describe":
        return {"jsonrpc": "2.0", "id": req_id, "result": MANIFEST}
    elif method == "extension.list_tools":
        return {"jsonrpc": "2.0", "id": req_id, "result": {"tools": MANIFEST["tools"]}}
    elif method == "extension.list_agents":
        return {"jsonrpc": "2.0", "id": req_id, "result": {"agents": []}}
    elif method == "extension.call_tool":
        params = req.get("params", {})
        tool_name = params.get("name", "")
        args = params.get("args", {})

        if tool_name == "set_thread_goal":
            result = set_thread_goal(
                thread_id=args.get("thread_id", ""),
                goal=args.get("goal", ""),
                criteria=args.get("criteria"),
            )
        elif tool_name == "get_thread_goal":
            result = get_thread_goal(args.get("thread_id", ""))
        elif tool_name == "complete_thread_goal":
            result = complete_thread_goal(
                args.get("thread_id", ""),
                args.get("summary", ""),
            )
        elif tool_name == "list_thread_goals":
            result = list_thread_goals()
        else:
            result = {"error": f"unknown tool: {tool_name}"}

        return {"jsonrpc": "2.0", "id": req_id, "result": result}
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
