package testutil

// Scenarios put a repo into a state worth reviewing, so tests can describe
// intent ("an agent just changed these files") instead of a sequence of git
// commands.

// ApplyFixture commits the fixture's pre-image and leaves its post-image as an
// unstaged working-tree change — the state differ sees after an external edit.
func (r *Repo) ApplyFixture(f DiffFixture) *Repo {
	r.t.Helper()

	switch {
	case f.Before == "":
		// Added file: nothing to commit, the file is simply new.
		r.CommitFile(".keep", "", "init")
		r.Write(f.NewPath(), f.After)
	case f.After == "":
		// Deleted file: commit it, then remove it from the working tree.
		r.CommitFile(f.Path, f.Before, "init")
		r.RemoveFile(f.Path)
	case f.To != "":
		r.CommitFile(f.Path, f.Before, "init")
		r.Rename(f.Path, f.To)
	default:
		r.CommitFile(f.Path, f.Before, "init")
		r.ExternalEdit(f.Path, f.After)
	}
	return r
}

// AgentChangeset builds a multi-file changeset resembling what a coding agent
// leaves behind: several modified files, one staged, one new, one deleted.
// It is deliberately small enough to assert against and varied enough to
// exercise file-list grouping, review state and diff navigation.
func (r *Repo) AgentChangeset() *Repo {
	r.t.Helper()

	r.Write("src/auth/login.ts", tsBefore)
	r.Write("src/api/client.ts", "export async function get(url: string) {\n  return fetch(url)\n}\n")
	r.Write("src/legacy/old.ts", "export const deprecated = true\n")
	r.Write("README.md", "# demo\n")
	r.Stage().Commit("baseline")

	// Unstaged edit, the common case.
	r.ExternalEdit("src/auth/login.ts", tsAfter)

	// Staged edit, to prove review state is independent of the index.
	r.Write("src/api/client.ts", "export async function get(url: string) {\n  return await fetch(url)\n}\n")
	r.Stage("src/api/client.ts")

	// New file, untracked.
	r.Untracked("src/utils/format.ts", "export const fmt = (s: string) => s.trim()\n")

	// Deleted file.
	r.RemoveFile("src/legacy/old.ts")

	return r
}
