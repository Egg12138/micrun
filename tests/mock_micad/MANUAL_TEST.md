# Manual Testing Guide for mock_micad

## Quick Start

1. Build mock_micad:
```bash
cd micrun/tests/mock_micad
make
```

2. Start mock_micad:
```bash
./mock_micad
```

You should see:
```
[INFO] Mock micad starting...
[INFO] Socket created and listening: /tmp/mica/mica-create.socket
[INFO] Epoll thread started
[INFO] Mock micad started successfully
[INFO] Main socket: /tmp/mica/mica-create.socket
[INFO] Press Ctrl+C to stop
```

Leave this terminal running and open another terminal for testing.

## Test 1: Create Client using Native C Send

Create a small C program to send create message:

```c
// create_client.c
#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>
#include <stdbool.h>

#define MAX_NAME_LEN 32
#define MAX_FIRMWARE_PATH_LEN 128
#define MAX_CPUSTR_LEN 128
#define MAX_IOMEM_LEN 512
#define MAX_NETWORK_LEN 512

struct create_msg {
    char name[MAX_NAME_LEN];
    char path[MAX_FIRMWARE_PATH_LEN];
    char ped[MAX_NAME_LEN];
    char ped_cfg[MAX_FIRMWARE_PATH_LEN];
    bool debug;
    char cpu_str[MAX_CPUSTR_LEN];
    int vcpu_num;
    int max_vcpu_num;
    int cpu_weight;
    int cpu_capacity;
    int memory;
    int max_memory;
    char iomem[MAX_IOMEM_LEN];
    char network[MAX_NETWORK_LEN];
};

int main() {
    int sock;
    struct sockaddr_un addr;
    struct create_msg msg;

    sock = socket(AF_UNIX, SOCK_STREAM, 0);
    if (sock < 0) {
        perror("socket");
        return 1;
    }

    memset(&addr, 0, sizeof(addr));
    addr.sun_family = AF_UNIX;
    strncpy(addr.sun_path, "/tmp/mica/mica-create.socket", sizeof(addr.sun_path)-1);

    if (connect(sock, (struct sockaddr*)&addr, sizeof(addr)) < 0) {
        perror("connect");
        close(sock);
        return 1;
    }

    memset(&msg, 0, sizeof(msg));
    strncpy(msg.name, "test-client", MAX_NAME_LEN-1);
    strncpy(msg.path, "/tmp/firmware.elf", MAX_FIRMWARE_PATH_LEN-1);
    strncpy(msg.ped, "xen", MAX_NAME_LEN-1);
    msg.debug = true;
    strncpy(msg.cpu_str, "1,2,3", MAX_CPUSTR_LEN-1);
    msg.vcpu_num = 1;
    msg.cpu_weight = 100;

    if (send(sock, &msg, sizeof(msg), 0) < 0) {
        perror("send");
        close(sock);
        return 1;
    }

    close(sock);
    printf("Client creation message sent\n");
    return 0;
}
```

Compile and run:
```bash
gcc -o create_client create_client.c
./create_client
```

In mock_micad terminal, you should see:
```
[PACKET] Received 1084 bytes on create socket
[PACKET] Received data (1084 bytes):
[hex dump]
[PACKET] As string: 'test-client\0\0\0.../__tmp_firmware.elf\0\0\0...'
[INFO] === Create Message Details ===
[INFO] Name: 'test-client'
[INFO] Path: '/tmp/firmware.elf'
...
[INFO] Registered client 'test-client' with status 'Created'
[INFO] Created client socket: /tmp/mica/test-client.socket
[INFO] PTY created for client 'test-client':
[INFO]   Slave: /dev/pts/N
[INFO]   Symlink: /tmp/mica/ttyRPMSG_test-client
[INFO]   Shell PID: XXXXX
```

Verify created resources:
```bash
# Check client socket
ls -l /tmp/mica/test-client.socket

# Check PTY symlink
ls -l /tmp/mica/ttyRPMSG_test-client
readlink /tmp/mica/ttyRPMSG_test-client  # Should show /dev/pts/N

# Check shell process
ps aux | grep "bash"
pstree -p | grep -A 5 mock_micad
```

## Test 2: Control Commands

Send control commands to client socket:

### Check Status

Use netcat or socat:
```bash
echo "status" | nc -U /tmp/mica/test-client.socket
```

Or using socat:
```bash
echo "status" | socat - UNIX-CONNECT:/tmp/mica/test-client.socket
```

mock_micad should show:
```
[PACKET] Control command for 'test-client': status
[INFO] Status for client 'test-client': Created, PID=XXXXX, PTY=/tmp/mica/ttyRPMSG_test-client
```

### Start Client

```bash
echo "start" | nc -U /tmp/mica/test-client.socket
```

You should see:
```
[PACKET] Control command for 'test-client': start
[INFO] Client 'test-client' status changed to 'Running'
```

### Stop Client

```bash
echo "stop" | nc -U /tmp/mica/test-client.socket
```

You should see:
```
[PACKET] Control command for 'test-client': stop
[INFO] Terminating shell for client 'test-client' (PID XXXXX)
[INFO] Client 'test-client' status changed to 'Stopped'
```

Verify shell terminated:
```bash
ps aux | grep XXXXX  # Should not show the shell
```

### Start Again

```bash
echo "start" | nc -U /tmp/mica/test-client.socket
```

A new shell process should be created with a new PID.

### Remove Client

```bash
echo "rm" | nc -U /tmp/mica/test-client.socket
```

You should see:
```
[PACKET] Control command for 'test-client': rm
[INFO] Removing client 'test-client'
[INFO] Terminating shell for client 'test-client' (PID XXXXX)
[INFO] Shell terminated for client 'test-client'
[INFO] Destroyed PTY for client 'test-client'
[INFO] Removed socket: /tmp/mica/test-client.socket
[INFO] Removed client 'test-client'
```

Verify cleanup:
```bash
ls -l /tmp/mica/test-client.socket  # Should not exist
ls -l /tmp/mica/ttyRPMSG_test-client  # Should not exist
```

## Test 3: Multiple Clients

Create multiple clients to test concurrent operation:

```bash
# Terminal 1: mock_micad
./mock_micad

# Terminal 2: Create clients
./create_client  # Edit to change name for each run
# Or modify create_client.c to accept command-line arguments
```

Or use quick text mode (if supported):
```bash
echo "create client2" | nc -U /tmp/mica/mica-create.socket
echo "create client3" | nc -U /tmp/mica/mica-create.socket
```

Check all clients:
```bash
ls -l /tmp/mica/*.socket
ls -l /tmp/mica/ttyRPMSG_*
```

Check status of all clients:
```bash
for client in client1 client2 client3; do
    echo "status" | nc -U /tmp/mica/$client.socket
done
```

## Test 4: PTY I/O

Test input/output through PTY:

```bash
# Terminal 1: mock_micad
./mock_micad

# Terminal 2: Create client
echo "create client-io" | nc -U /tmp/mica/mica-create.socket

# Terminal 3: Connect to PTY
socat - /dev/pts/N  # Use the PTS number shown in mock_micad output

# Or read from PTY
cat /dev/pts/N

# Type commands in Terminal 3 and see shell output
```

## Test 5: Lifecycle Test

Full lifecycle in one test:

```bash
# Terminal 1
./mock_micad

# Terminal 2
echo "create lifecycle-test" | nc -U /tmp/mica/mica-create.socket
echo "status" | nc -U /tmp/mica/lifecycle-test.socket
echo "start" | nc -U /tmp/mica/lifecycle-test.socket
echo "status" | nc -U /tmp/mica/lifecycle-test.socket
sleep 2
echo "stop" | nc -U /tmp/mica/lifecycle-test.socket
echo "status" | nc -U /tmp/mica/lifecycle-test.socket
echo "rm" | nc -U /tmp/mica/lifecycle-test.socket
```

## Test 6: Error Cases

Test error handling:

### Try to create duplicate client
```bash
echo "create dup-client" | nc -U /tmp/mica/mica-create.socket
echo "create dup-client" | nc -U /tmp/mica/mica-create.socket
```
Should show error: "Client 'dup-client' already exists"

### Try to control non-existent client
```bash
echo "status" | nc -U /tmp/mica/nonexistent.socket  # Socket doesn't exist
```
Should show connection error

### Try to start already running client
```bash
echo "create start-twice" | nc -U /tmp/mica/mica-create.socket
echo "start" | nc -U /tmp/mica/start-twice.socket
echo "start" | nc -U /tmp/mica/start-twice.socket  # Second time
```
Should show: "Client 'start-twice' is already Running"

### Try to stop created (not running) client
```bash
echo "create not-running" | nc -U /tmp/mica/mica-create.socket
echo "stop" | nc -U /tmp/mica/not-running.socket
```
Should show: "Cannot stop client 'not-running' in 'Created' state"

## Test 7: Signal Handling

Test graceful shutdown:

```bash
# Terminal 1
./mock_micad

# Terminal 2
echo "create sig-test" | nc -U /tmp/mica/mica-create.socket
ps aux | grep bash  # Note PIDs

# Terminal 1: Press Ctrl+C
```

All shell processes should terminate. Verify:
```bash
ps aux | grep bash  # Should not show the test clients
ls -l /tmp/mica/    # Should be empty or only mica-create.socket
```

Or send signal:
```bash
# Terminal 1
./mock_micad &
PID=$!

# Terminal 2
echo "create sig-test2" | nc -U /tmp/mica/mica-create.socket

# Terminal 1
kill -TERM $PID

# Verify cleanup
wait $PID 2>/dev/null
ls -l /tmp/mica/
ps aux | grep sig-test2  # Should not show
```

## Test 8: Debug Mode

Check debug output:

On another terminal, watch mock_micad output:
```bash
tail -f /dev/pts/XXX  # Watch mock_micad terminal
```

Create client and verify:
- Hex dump of received data
- Parsed message fields
- Client registration
- PTY creation details
- Shell PID

## Troubleshooting

### Cannot connect to socket
```bash
# Check if mock_micad is running
ps aux | grep mock_micad

# Check socket permissions
ls -l /tmp/mica/mica-create.socket

# Check if socket exists
netstat -an | grep mica-create.socket

# Try running as root (if permission issue)
sudo ./mock_micad
```

### Client creation fails
```bash
# Check if client already exists
ls -l /tmp/mica/*.socket

# Remove existing
for s in /tmp/mica/*.socket; do [ -e "$s" ] && rm "$s"; done

# Check mock_micad output for errors
```

### Shell not starting
```bash
# Check bash is available
which bash
which sh

# Try simpler shell
# Edit mock_micad.c to use /bin/sh instead
```

### PTY creation fails
```bash
# Check /dev/pts exists
ls -l /dev/pts

# Check PTY permissions
ls -l /dev/ptmx

# Check max PTY limit
cat /proc/sys/kernel/pty/max
cat /proc/sys/kernel/pty/nr
```

### Zombie processes
```bash
# If shells don't terminate properly
ps aux | grep defunct

# Kill parent (mock_micad) to clean up
pkill -9 mock_micad

# Manually cleanup
make clean-all
```

### Port already in use
```bash
# Find process using socket
lsof /tmp/mica/mica-create.socket

# Kill it
fuser -k /tmp/mica/mica-create.socket
```

## Automated Testing

Use the Python test script for comprehensive testing:

```bash
# Terminal 1
./mock_micad

# Terminal 2
python3 test_mock.py
```

Or if Python not available, use the shell script in Makefile test target.

## Expected Output Summary

Successful test sequence should show in mock_micad output:

```
[INFO] Mock micad starting...
[INFO] Socket created and listening: /tmp/mica/mica-create.socket
[INFO] Epoll thread started
[INFO] Mock micad started successfully
[INFO] Main socket: /tmp/mica/mica-create.socket
[INFO] Press Ctrl+C to stop

[PACKET] Received 1084 bytes on create socket
[PACKET] Received data (1084 bytes):
00 00 00 ...
[PACKET] As string: 'test-client\0...'
[INFO] === Create Message Details ===
[INFO] Name: 'test-client'
...
[INFO] Registered client 'test-client' with status 'Created'
[INFO] Created client socket: /tmp/mica/test-client.socket
[INFO] PTY created for client 'test-client':
[INFO]   Slave: /dev/pts/N
[INFO]   Symlink: /tmp/mica/ttyRPMSG_test-client
[INFO]   Shell PID: XXXXX

[PACKET] Control command for 'test-client': status
[INFO] Status for client 'test-client': Created, PID=XXXXX, PTY=/tmp/mica/ttyRPMSG_test-client

[PACKET] Control command for 'test-client': start
[INFO] Client 'test-client' status changed to 'Running'

[PACKET] Control command for 'test-client': stop
[INFO] Terminating shell for client 'test-client' (PID XXXXX)
[INFO] Client 'test-client' status changed to 'Stopped'

[PACKET] Control command for 'test-client': rm
[INFO] Removing client 'test-client'
[INFO] Terminating shell for client 'test-client' (PID XXXXX)
[INFO] Shell terminated for client 'test-client'
[INFO] Destroyed PTY for client 'test-client'
[INFO] Removed socket: /tmp/mica/test-client.socket
[INFO] Removed client 'test-client'

(^C pressed)
[INFO] Received signal 2, shutting down...
[INFO] === Starting cleanup ===
...
[INFO] Mock micad stopped
```

## Success Criteria

✓ mock_micad starts without errors
✓ Can create client with binary protocol
✓ Client socket is created
✓ PTY symlink is created pointing to /dev/pts/N
✓ Shell process is spawned
✓ Can send status command and get response
✓ Can start/stop client
✓ Can remove client (cleanup everything)
✓ Ctrl+C terminates gracefully (all processes stopped)
✓ No resource leaks (sockets cleaned up)
✓ Can create multiple clients simultaneously
