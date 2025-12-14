#include <stdlib.h>
#include <fcntl.h>
#include <unistd.h>
#include <pty.h>

int main() {
    int master_fd, slave_fd;
    char *slave_name;

    // Open a pseudo-terminal master
    master_fd = posix_openpt(O_RDWR | O_NOCTTY);

    // Grant access to the slave pseudo-terminal
    grantpt(master_fd);

    // Unlock the pseudo-terminal
    unlockpt(master_fd);

    // Get the name of the slave pseudo-terminal
    slave_name = ptsname(master_fd);

    // Open the slave pseudo-terminal
    slave_fd = open(slave_name, O_RDWR);

    // Fork and exec a shell
    if (fork() == 0) {
        // Child process
        close(master_fd);
        dup2(slave_fd, STDIN_FILENO);
        dup2(slave_fd, STDOUT_FILENO);
        dup2(slave_fd, STDERR_FILENO);
        close(slave_fd);

        execl("/bin/bash", "bash", NULL);
    }

    // Parent process can now communicate with the shell
    // through master_fd

    return 0;
}
