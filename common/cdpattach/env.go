// SPDX-License-Identifier: Apache-2.0

package cdpattach

import (
	"fmt"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// UserDataDirEnv names the Chrome user data directory a collector attaches
// through. Unset means Chrome's default directory under HOME; set and empty
// is refused.
const UserDataDirEnv = "KNOWLEDGE_CHROME_USER_DATA_DIR"

// homeEnv is the variable the default directory is read under.
const homeEnv = "HOME"

// Environment is the environment declaration every collector built on this
// package declares whole: the user data directory variable (path class,
// empty-sensitive) and HOME (path class).
func Environment() []framework.EnvDeclaration {
	return []framework.EnvDeclaration{
		{
			Name: UserDataDirEnv, Class: framework.EnvClassPath, EmptySensitive: true,
			Description: "the user data directory of the Chrome you are signed in with; unset reads Chrome's default directory under HOME",
		},
		{
			Name: homeEnv, Class: framework.EnvClassPath,
			Description: "the home directory Chrome's default user data directory is under",
		},
	}
}

// UserDataDir resolves the Chrome user data directory from lookup (os.LookupEnv
// in a collector): UserDataDirEnv when set and non-empty, Chrome's default
// under HOME when it is unset. Set and empty, and unset with no HOME, are
// errors naming the variables.
func UserDataDir(lookup func(string) (string, bool)) (string, error) {
	if dir, ok := lookup(UserDataDirEnv); ok {
		if dir == "" {
			return "", fmt.Errorf("%s is set and empty: set it to the user data directory of the Chrome you are signed in with, or unset it to use Chrome's default", UserDataDirEnv)
		}
		return dir, nil
	}
	home, ok := lookup(homeEnv)
	if !ok || home == "" {
		return "", fmt.Errorf("neither %s nor %s is set: set %s to the user data directory of the Chrome you are signed in with", UserDataDirEnv, homeEnv, UserDataDirEnv)
	}
	return DefaultUserDataDir(home)
}
