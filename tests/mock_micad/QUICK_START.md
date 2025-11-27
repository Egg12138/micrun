# Mock Micad - Quick Start Guide

## 🚀 Quick Start (5 minutes)

### Step 1: Build
```bash
cd /home/egg/oee/cleanspace/src/mcs/micrun/tests/mock_micad
make
```

### Step 2: Run
```bash
./mock_micad
```

You should see:
```
[INFO] Mock micad starting...
[INFO] Socket created and listening: /tmp/mica/mica-create.socket
[INFO] Mock micad started successfully
[INFO] Main socket: /tmp/mica/mica-create.socket
[INFO] Press Ctrl+C to stop
[INFO] No clients registered
[INFO] Epoll thread started
```

### Step 3: Create a Client (in another terminal)
```bash
echo "create my-client" | socat - UNIX-CONNECT:/tmp/mica/mica-create.socket
```

You should see in mock_micad output:
```
[INFO] Creating client via text command: 'my-client'
[INFO] Socket created and listening: /tmp/mica/my-client.socket
[INFO] Created client socket: /tmp/mica/my-client.socket
[INFO] Registered client 'my-client' with status 'Created'
[INFO] PTY created for client 'my-client':
[INFO]   Slave: /dev/pts/N
[INFO]   Symlink: /tmp/mica/ttyRPMSG_my-client
[INFO]   Shell PID: XXXXX
[INFO] Successfully created client 'my-client' via text command
```

Verify resources:
```bash
ls -l /tmp/mica/my-client.socket          # Client socket
ls -l /tmp/mica/ttyRPMSG_my-client        # PTY symlink
ps aux | grep bash | grep my-client       # Shell process
```

### Step 4: Control the Client

**Check status:**
```bash
echo "status" | socat - UNIX-CONNECT:/tmp/mica/mica-create.socket
```

Output:
```
[INFO] === Client Status List ===
[INFO] Client 0: name='my-client', status='Created', pid=XXXXX, pty=/tmp/mica/ttyRPMSG_my-client
[INFO] === Total: 1 clients ===
```

**Start the client:**
```bash
echo "start" | socat - UNIX-CONNECT:/tmp/mica/my-client.socket
```

**Stop the client:**
```bash
echo "stop" | socat - UNIX-CONNECT:/tmp/mica/my-client.socket
```

**Remove the client:**
```bash
echo "rm" | socat - UNIX-CONNECT:/tmp/mica/my-client.socket
```

### Step 5: Interactive PTY

Connect to the shell:
```bash
# Find the PTS number from mock_micad output
# or read the symlink:
readlink /tmp/mica/ttyRPMSG_my-client
# Output: /dev/pts/N

# Connect to the PTY
socat - /dev/pts/N

# Now you can type shell commands!
# Try: ls -la
#      pwd
#      echo "Hello from mock micad"
```

### Step 6: Multiple Clients

Create more clients:
```bash
echo "create client-2" | socat - UNIX-CONNECT:/tmp/mica/mica-create.socket
echo "create client-3" | socat - UNIX-CONNECT:/tmp/mica/mica-create.socket
```

Check all clients:
```bash
echo "status" | socat - UNIX-CONNECT:/tmp/mica/mica-create.socket
```

Control each client independently:
```bash
echo "stop" | socat - UNIX-CONNECT:/tmp/mica/client-2.socket
echo "start" | socat - UNIX-CONNECT:/tmp/mica/client-3.socket
```

### Step 7: Cleanup

Stop mock_micad (Ctrl+C in terminal where it's running):
```
^C
[INFO] Received signal 2, shutting down...
[INFO] Epoll thread exiting
[INFO] === Starting cleanup ===
[INFO] === Cleanup completed ===
[INFO] Mock micad stopped
```

Or manually:
```bash
killall mock_micad
```

Clean up leftover resources:
```bash
make clean-all
```

## 📚 Common Commands

### Create
```bash
echo "create <name>" | socat - UNIX-CONNECT:/tmp/mica/mica-create.socket
```

### Status
```bash
echo "status" | socat - UNIX-CONNECT:/tmp/mica/mica-create.socket
```

### Start
```bash
echo "start" | socat - UNIX-CONNECT:/tmp/mica/<name>.socket
```

### Stop
```bash
echo "stop" | socat - UNIX-CONNECT:/tmp/mica/<name>.socket
```

### Remove
```bash
echo "rm" | socat - UNIX-CONNECT:/tmp/mica/<name>.socket
```

### Connect to PTY
```bash
# Find PTS device
readlink /tmp/mica/ttyRPMSG_<name>

# Connect
socat - /dev/pts/N

# Or use a simpler read
cat /dev/pts/N
```

## 🔍 Debug Output

Mock micad logs everything:

**Received packets:**
```
[PACKET] Received 13 bytes on create socket
[PACKET] Received data (13 bytes):
63 72 65 61 74 65 20 74 65 73 74 31 0a
[PACKET] As string: 'create test1\x0a'
```

**Command execution:**
```
[INFO] Creating client via text command: 'test1'
[INFO] Socket created and listening: /tmp/mica/test1.socket
[INFO] PTY created for client 'test1':
[INFO]   Slave: /dev/pts/10
[INFO]   Symlink: /tmp/mica/ttyRPMSG_test1
[INFO]   Shell PID: 2530334
```

**Status display:**
```
[INFO] Status command received on create socket
[INFO] === Client Status List ===
[INFO] Client 0: name='test1', status='Created', pid=2530334, pty=/tmp/mica/ttyRPMSG_test1
[INFO] === Total: 1 clients ===
```

## 🧪 Run Tests

Automated test:
```bash
./test_simple.sh
```

Live demo:
```bash
./demo.sh
```

## 🐛 Troubleshooting

**"Connection refused"**
- Check mock_micad is running
- Check socket exists: `ls -l /tmp/mica/mica-create.socket`

**"Cannot use port with -U" (nc error)**
- Use `socat` instead of `nc -U`:
  ```bash
  echo "create test" | socat - UNIX-CONNECT:/tmp/mica/mica-create.socket
  ```

**"No such file or directory"**
- Create socket directory: `mkdir -p /tmp/mica`

**Resources not cleaned up**
```bash
make clean-all
```

## 📄 Files

- `mock_micad.c` - Main implementation
- `Makefile` - Build configuration
- `test_simple.sh` - Simple functional tests
- `demo.sh` - Live demonstration script
- `README.md` - Full documentation
- `FEATURES.md` - Features summary
- `MANUAL_TEST.md` - Manual testing guide
- `QUICK_START.md` - This file

## 🎓 Learn More

- Read `FEATURES.md` for complete feature list
- Read `MANUAL_TEST.md` for detailed testing procedures
- Read `README.md` for full documentation

## ✅ Next Steps

1. Try creating multiple clients
2. Test shell I/O through PTY
3. Run automated tests
4. Integrate with micrun
5. Use for development/debugging

Happy testing! 🚀
