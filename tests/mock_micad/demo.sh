#!/bin/bash
# Live demonstration of mock_micad

set -e

echo "╔═══════════════════════════════════════════════════════════════╗"
echo "║           MOCK MICAD - LIVE DEMONSTRATION                     ║"
echo "╚═══════════════════════════════════════════════════════════════╝"
echo ""

# Start mock_micad in background with visible output
echo "🚀 Starting mock_micad..."
echo ""
./mock_micad &
MOCK_PID=$!
sleep 2

# Function to send command and show output
send_cmd() {
    local socket=$1
    local cmd=$2
    local desc=$3

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "📤 $desc"
    echo "   Command: $cmd"
    echo "   Socket:  $socket"
    echo ""
    echo "$cmd" | socat - UNIX-CONNECT:$socket
    echo ""
    sleep 1
}

# Demo 1: Create multiple clients
echo "📝 DEMO 1: Creating Multiple Clients"
echo ""
send_cmd "/tmp/mica/mica-create.socket" "create client-a" "Create client 'client-a'"
send_cmd "/tmp/mica/mica-create.socket" "create client-b" "Create client 'client-b'"
send_cmd "/tmp/mica/mica-create.socket" "create client-c" "Create client 'client-c'"

# Demo 2: Check status
echo "📊 DEMO 2: Check All Clients Status"
echo ""
send_cmd "/tmp/mica/mica-create.socket" "status" "List all clients"

# Demo 3: Control operations
echo "⚙️  DEMO 3: Client Lifecycle Management"
echo ""
send_cmd "/tmp/mica/client-a.socket" "start" "Start client-a (already running)"
send_cmd "/tmp/mica/client-a.socket" "stop" "Stop client-a"
send_cmd "/tmp/mica/client-a.socket" "start" "Restart client-a"
send_cmd "/tmp/mica/client-b.socket" "stop" "Stop client-b"

# Demo 4: Show PTY symlinks
echo "🔌 DEMO 4: PTY Resources Created"
echo ""
echo "Client sockets:"
ls -lh /tmp/mica/*.socket 2>/dev/null || echo "   (none)"
echo ""
echo "PTY symlinks:"
ls -lh /tmp/mica/ttyRPMSG_* 2>/dev/null || echo "   (none)"
echo ""
echo "Shell processes:"
ps aux | grep -E "(bash|sh)" | grep -v grep || echo "   (none)"
echo ""

# Demo 5: Remove clients
echo "🧹 DEMO 5: Removing Clients"
echo ""
send_cmd "/tmp/mica/client-b.socket" "rm" "Remove client-b completely"
send_cmd "/tmp/mica/mica-create.socket" "status" "Remaining clients"

# Cleanup
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🧹 Cleaning up..."
kill -TERM $MOCK_PID
sleep 1

echo ""
echo "╔═══════════════════════════════════════════════════════════════╗"
echo "║                    DEMO COMPLETE! ✓                           ║"
echo "╚═══════════════════════════════════════════════════════════════╝"
echo ""
