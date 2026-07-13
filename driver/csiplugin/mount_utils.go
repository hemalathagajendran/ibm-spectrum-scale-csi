//go:build linux
// +build linux

/**
 * Copyright 2019, 2024 IBM Corp.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package scale

import (
	"fmt"
	"os/exec"

	"golang.org/x/sys/unix"
)

// bindMount performs a bind mount of source onto target using the system
// mount(8) binary, mirroring the behaviour of mount.Mounter.Mount with the
// "bind" option:
//
//  1. First pass: mount --bind <source> <target>
//  2. Second pass: mount --bind --remount <source> <target>
//     with any read-only / nodev / noexec / nosuid / noatime / relatime /
//     nodiratime flags inherited from the source filesystem via statfs(2).
//
// Using the /host-prefixed source path ensures that the statfs(2) call
// succeeds inside the container (the container filesystem only exposes
// paths under /host).
func bindMount(source, target string) error {
	// First pass: establish the bind mount.
	if out, err := exec.Command("mount", "--bind", source, target).CombinedOutput(); err != nil {
		return fmt.Errorf("mount --bind %s %s failed: %v\nOutput: %s", source, target, err, string(out))
	}

	// Derive the mount flags that are active on the source filesystem so that
	// the bind mount inherits them (e.g. ro, nodev, nosuid, noexec, …).
	remountOpts := []string{"--bind", "--remount"}
	var st unix.Statfs_t
	if err := unix.Statfs(source, &st); err == nil {
		flagMap := map[int]string{
			unix.MS_RDONLY:     "ro",
			unix.MS_NODEV:      "nodev",
			unix.MS_NOEXEC:     "noexec",
			unix.MS_NOSUID:     "nosuid",
			unix.MS_NOATIME:    "noatime",
			unix.MS_RELATIME:   "relatime",
			unix.MS_NODIRATIME: "nodiratime",
		}
		for flag, opt := range flagMap {
			if int(st.Flags)&flag == flag {
				remountOpts = append(remountOpts, "-o", opt)
			}
		}
	}

	// Second pass: remount to propagate inherited flags.
	remountArgs := append(remountOpts, source, target)
	if out, err := exec.Command("mount", remountArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("mount --bind --remount %s %s failed: %v\nOutput: %s", source, target, err, string(out))
	}

	return nil
}
