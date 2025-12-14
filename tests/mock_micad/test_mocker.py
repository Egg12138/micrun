#!/usr/bin/env python3
"""
Test script for mocker.py
"""

import os
import sys
import time
import socket
import struct
import threading
import subprocess
from pathlib import Path

# Add current directory to path
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from mocker import Mocker, CREATE_MSG_FORMAT, MAX_NAME_LEN, MAX_FIRMWARE_PATH_LEN, MAX_PED_LEN, MAX_CPUSTR_LEN, MAX_IOMEM_LEN, MAX_NETWORK_LEN

def test_create_client():
    """Test creating a client via mocker."""
    print("Starting mocker...")
    mocker = Mocker(socket_dir="/tmp/test-mica")

    # Start in a thread
    mocker_thread = threading.Thread(target=mocker.run, daemon=True)
    mocker_thread.start()

    # Wait for startup
    time.sleep(2)

    # Check if mica-create.socket exists
    create_socket = "/tmp/test-mica/mica-create.socket"
    if not os.path.exists(create_socket):
        print("ERROR: mica-create.socket not created")
        return False

    print("✓ mica-create.socket created")

    # Prepare create message (similar to mica.py)
    name = "test-client"
    path = "/tmp/test/firmware.elf"
    ped = "xen"
    ped_cfg = "/tmp/test/config"
    debug = True
    cpu_str = "1,2,3"
    vcpu_num = 1
    max_vcpu_num = 2
    cpu_weight = 100
    cpu_capacity = 50
    memory = 128
    max_memory = 256
    iomem = ""
    network = ""

    # Pack message
    data = struct.pack(CREATE_MSG_FORMAT,
                       name.ljust(MAX_NAME_LEN, '\0').encode(),
                       path.ljust(MAX_FIRMWARE_PATH_LEN, '\0').encode(),
                       ped.ljust(MAX_PED_LEN, '\0').encode(),
                       ped_cfg.ljust(MAX_FIRMWARE_PATH_LEN, '\0').encode(),
                       debug,
                       cpu_str.ljust(MAX_CPUSTR_LEN, '\0').encode(),
                       vcpu_num,
                       max_vcpu_num,
                       cpu_weight,
                       cpu_capacity,
                       memory,
                       max_memory,
                       iomem.ljust(MAX_IOMEM_LEN, '\0').encode(),
                       network.ljust(MAX_NETWORK_LEN, '\0').encode())

    # Send to socket
    try:
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        sock.connect(create_socket)
        sock.sendall(data)

        # Receive response
        response = b""
        while True:
            chunk = sock.recv(1024)
            if not chunk:
                break
            response += chunk
            if b"MICA-SUCCESS" in response or b"MICA-FAILED" in response:
                break

        sock.close()

        if b"MICA-SUCCESS" in response:
            print("✓ Create command succeeded")
        else:
            print(f"ERROR: Create failed: {response}")
            return False

    except Exception as e:
        print(f"ERROR: Socket communication failed: {e}")
        return False

    # Check client socket
    client_socket = f"/tmp/test-mica/{name}.socket"
    if not os.path.exists(client_socket):
        print(f"ERROR: Client socket not created: {client_socket}")
        return False
    print(f"✓ Client socket created: {client_socket}")

    # Check PTY symlink
    symlink_path = f"/tmp/test-mica/ttyRPMSG_{name}_0"
    if not os.path.exists(symlink_path):
        print(f"ERROR: PTY symlink not created: {symlink_path}")
        return False
    print(f"✓ PTY symlink created: {symlink_path}")

    # Test control commands
    commands = ["status", "stop", "start", "set foo bar", "gdb"]
    for cmd in commands:
        print(f"\nTesting command: {cmd}")
        try:
            sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            sock.connect(client_socket)
            sock.sendall(cmd.encode())

            response = b""
            while True:
                chunk = sock.recv(1024)
                if not chunk:
                    break
                response += chunk
                if b"MICA-SUCCESS" in response or b"MICA-FAILED" in response:
                    break

            sock.close()

            if b"MICA-SUCCESS" in response:
                print(f"  ✓ Command succeeded")
                if cmd == "gdb":
                    # Print gdb command
                    print(f"  GDB command: {response.split(b'MICA-SUCCESS')[0].decode().strip()}")
            else:
                print(f"  ✗ Command failed: {response}")

        except Exception as e:
            print(f"  ERROR: {e}")

    # Test rm command
    print("\nTesting rm command...")
    try:
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        sock.connect(client_socket)
        sock.sendall(b"rm")

        response = b""
        while True:
            chunk = sock.recv(1024)
            if not chunk:
                break
            response += chunk
            if b"MICA-SUCCESS" in response or b"MICA-FAILED" in response:
                break

        sock.close()

        if b"MICA-SUCCESS" in response:
            print("✓ rm command succeeded")
            # Verify cleanup
            if os.path.exists(client_socket):
                print(f"WARNING: Client socket still exists: {client_socket}")
            if os.path.exists(symlink_path):
                print(f"WARNING: PTY symlink still exists: {symlink_path}")
        else:
            print(f"✗ rm command failed: {response}")

    except Exception as e:
        print(f"ERROR: rm command failed: {e}")

    # Stop mocker
    print("\nStopping mocker...")
    mocker.stop()
    mocker_thread.join(timeout=2)

    # Cleanup test directory
    import shutil
    if os.path.exists("/tmp/test-mica"):
        shutil.rmtree("/tmp/test-mica", ignore_errors=True)

    print("\n=== Test completed ===")
    return True

if __name__ == "__main__":
    success = test_create_client()
    sys.exit(0 if success else 1)