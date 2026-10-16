#!/usr/bin/env python3
"""Claude Code PreToolUse hook for godi's Bash tool: refuse the full gates and
worktrees made outside .claude/worktrees/.

The hook's JSON arrives on stdin. Exit 0 lets the command run; exit 2 refuses
it, and Claude Code shows stderr to the session as the reason.

Refused, because CI runs them on every push and a shared Mac ran them into a
kernel panic on 2026-10-15:
  - the full gate: `make verify-full`, `make verify-ci` and their -unlocked
    steps, and the whole-tree targets `make test`, `lint`, `test-cover`,
    `published-check`, `security`, `vulncheck` and `benchmark`;
  - `go test` over a whole module (`./...` at a module root, `all`), or with
    -race across more than one package;
  - golangci-lint over a whole module (no package, or `./...` at a module
    root), on a file instead of a package, or with --allow-parallel-runners;
  - any of those through scripts/for-each-module.sh, which runs them in every
    module.
`make verify` checks what changed instead, and CI runs the rest.

Also refused: `git worktree add` to a path that is not directly under the main
checkout's .claude/worktrees/ (git names the main checkout; where it cannot,
any .claude/worktrees/ will do) or in a Claude scratchpad, because worktrees
beside the repository, inside another worktree or in /tmp are forgotten and
pile up.

CLAUDE_FULL_CHECKS=1 in the hook's own environment, which is the one Claude Code
was started with, lifts the gate refusals; the same assignment written in front
of the command does not, because the hook never reads the command's
environment. With CI set the hook refuses nothing. Anything the hook cannot
parse or work out is let through: a session kept from a command it needs is
worse off than one that runs a gate CI would have run anyway.
"""
import json
import os
import re
import shlex
import subprocess
import sys

GATE_ADVICE = (
    "Run `make verify` instead: it checks what this checkout changed (gofmt, build, and go vet, go test "
    "without -race and golangci-lint on the changed packages). CI runs the full gate on every push; read it "
    "with `gh pr checks <n> --watch` and `gh run view <id> --log-failed`. A person who wants the full gate "
    "here starts Claude Code with CLAUDE_FULL_CHECKS=1 in its environment."
)
GODI_MODULE = re.compile(r"github\.com/junioryono/godi(/|$)")
# A godi module's import path followed by /...: the core, github.com/junioryono/godi/v6, or
# an integration, github.com/junioryono/godi/<name>/v6, or a module without a major suffix.
WHOLE_MODULE_PATTERN = re.compile(r"github\.com/junioryono/godi(/[a-z]+)?(/v[0-9]+)?/\.\.\.")
MAKE_PROGRAMS = {"make", "gmake"}
FULL_GATE_TARGETS = {
    "verify-full", "verify-full-unlocked", "verify-ci", "verify-ci-unlocked", "test", "lint", "test-cover",
    "published-check", "security", "vulncheck", "benchmark",
}
MAKE_VALUE_OPTIONS = {
    "-C", "-f", "-I", "-o", "-W", "--directory", "--file", "--makefile", "--include-dir", "--old-file",
    "--assume-old", "--what-if", "--new-file", "--assume-new",
}
MAKE_OPTIONAL_NUMBER = {"--jobs", "--load-average", "--max-load"}
MAKE_DRY_RUN = {"--dry-run", "--just-print", "--recon", "--question", "--touch"}
GO_TEST_VALUE_FLAGS = {
    "run", "skip", "bench", "benchtime", "count", "cpu", "parallel", "timeout", "coverprofile", "covermode",
    "coverpkg", "blockprofile", "blockprofilerate", "cpuprofile", "memprofile", "memprofilerate", "mutexprofile",
    "mutexprofilefraction", "outputdir", "trace", "fuzz", "fuzztime", "fuzzminimizetime", "list", "shuffle", "vet",
    "exec", "o", "p", "tags", "ldflags", "gcflags", "asmflags", "mod", "modfile", "overlay", "pgo", "pkgdir",
    "toolexec", "installsuffix", "C", "compiler", "gccgoflags",
}
GOLANGCI_VALUE_FLAGS = {
    "-c", "--config", "-j", "--concurrency", "--timeout", "--build-tags", "--path-prefix", "--path-mode", "-E",
    "--enable", "-D", "--disable", "--enable-only", "--default", "--new-from-rev", "--new-from-patch",
    "--max-issues-per-linter", "--max-same-issues", "--modules-download-mode", "--go", "--color",
    "--output.text.path", "--output.json.path", "--output.tab.path", "--output.html.path",
    "--output.checkstyle.path", "--output.code-climate.path", "--output.junit-xml.path",
    "--output.teamcity.path", "--output.sarif.path",
}
# Programs that run the rest of their words as another command, with the
# options of each that take a value.
WRAPPERS = {
    "env": {"-u", "--unset", "-C", "--chdir", "-S", "--split-string"},
    "nice": {"-n", "--adjustment"},
    "nohup": set(),
    "time": set(),
    "command": set(),
    "exec": set(),
    "caffeinate": set(),
    "timeout": {"-s", "--signal", "-k", "--kill-after"},
    "gtimeout": {"-s", "--signal", "-k", "--kill-after"},
    "sudo": {"-u", "-g", "-D", "--chdir"},
}
WRAPPER_OPERAND = {"timeout", "gtimeout"}  # a duration comes before the program
SHELLS = {"sh", "bash", "zsh", "dash"}
SEPARATORS = ";&|()\n"
RESERVED = {"if", "then", "else", "elif", "do", "while", "until", "!", "{", "}", "fi", "done", "time"}
REDIRECTION = re.compile(r"(\d*|&)(>>|>\||<>|>&|<&|>|<)(.*)")
HEREDOC = re.compile(r"<<-?[ \t]*(['\"]?)([A-Za-z_][A-Za-z0-9_]*)\1")
WORKTREE_VALUE_OPTIONS = {"-b", "-B", "--reason"}
SCRATCHPAD = re.compile(r"/(private/)?tmp/claude-[^/]+/(.+/)?scratchpad(/.*)?")


class Refused(Exception):
    pass


def refuse(reason):
    raise Refused(reason)


def strip_heredocs(text):
    """Drop heredoc bodies, which the shell feeds to a command and never runs."""
    lines, out, pending = text.split("\n"), [], []
    for line in lines:
        if pending:
            if line.lstrip("\t") == pending[0]:
                pending.pop(0)
            continue
        out.append(line)
        pending.extend(m.group(2) for m in HEREDOC.finditer(line))
    return "\n".join(out)


def strip_comments(text):
    """Drop comments: from a # that starts a word outside quotes to the end of
    its line."""
    # word_start is whether the next character starts a word: true after an
    # unquoted, unescaped blank or operator, never after an escaped one.
    out, quote, i, n, word_start = [], None, 0, len(text), True
    while i < n:
        c = text[i]
        if quote:
            out.append(c)
            if c == "\\" and quote == '"' and i + 1 < n:
                out.append(text[i + 1])
                i += 1
            elif c == quote:
                quote = None
            word_start = False
        elif c == "\\" and i + 1 < n:
            out.append(text[i : i + 2])
            i += 1
            word_start = False
        elif c in "'\"":
            quote = c
            out.append(c)
            word_start = False
        elif c == "#" and word_start:
            while i < n and text[i] != "\n":
                i += 1
            continue
        else:
            out.append(c)
            word_start = c in " \t\n;&|()"
        i += 1
    return "".join(out)


def simple_commands(text):
    """The command line as a list of events: a simple command's words, or "("
    and ")" where a subshell opens and closes."""
    text = strip_comments(strip_heredocs(text).replace("\\\n", ""))
    lexer = shlex.shlex(text, posix=True, punctuation_chars=SEPARATORS)
    lexer.whitespace = " \t\r"
    lexer.whitespace_split = True
    lexer.commenters = ""
    out, current = [], []
    for word in lexer:
        if word and set(word) <= set(SEPARATORS):
            if current:
                out.append(current)
            current = []
            out.extend(c for c in word if c in "()")
        else:
            current.append(word)
    if current:
        out.append(current)
    return out


def unwrap(words):
    """Strip reserved words, assignments and wrappers in front of the program,
    and return the directory a wrapper runs it in (env -C), if any."""
    chdir = None
    while words:
        if words[0] in RESERVED:
            words = words[1:]
            continue
        if re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*=.*", words[0]):
            words = words[1:]
            continue
        name = os.path.basename(words[0])
        if name not in WRAPPERS:
            break
        valued = WRAPPERS[name]
        words = words[1:]
        while words and words[0].startswith("-") and words[0] != "--":
            opt = words.pop(0)
            flag, eq, value = opt.partition("=")
            if eq and flag == "--chdir":
                chdir = value
            elif opt in valued and words and not eq:
                value = words.pop(0)
                if opt in ("-C", "--chdir", "-D"):
                    chdir = value
            elif re.fullmatch(r"-\d+", opt) and name == "nice":
                continue
        if words and words[0] == "--":
            words = words[1:]
        if name in WRAPPER_OPERAND and words:
            words = words[1:]
    return words, chdir


def drop_redirections(words):
    out, skip = [], False
    for w in words:
        if skip:
            skip = False
            continue
        m = REDIRECTION.fullmatch(w)
        if m:
            skip = not m.group(3)
            continue
        out.append(w)
    return out


def commands(text, cwd):
    """Every simple command the line runs, with the directory it runs in,
    following cd, subshells and `sh -c`."""
    found, saved = [], []
    for event in simple_commands(text):
        if event == "(":
            saved.append(cwd)
            continue
        if event == ")":
            if saved:
                cwd = saved.pop()
            continue
        words, chdir = unwrap(drop_redirections(event))
        if not words:
            continue
        here = os.path.join(cwd, os.path.expanduser(chdir)) if chdir else cwd
        program = os.path.basename(words[0])
        if program in SHELLS:
            for i, a in enumerate(words[1:], start=1):
                if re.fullmatch(r"-[A-Za-z]*c[A-Za-z]*", a) and i + 1 < len(words):
                    found.extend(commands(words[i + 1], here))
                    break
            continue
        if program == "cd":
            if len(words) == 1:
                cwd = os.path.expanduser("~")
            elif words[1] != "-":
                cwd = os.path.join(cwd, os.path.expanduser(words[1]))
            continue
        found.append((words, here))
    return found


def module_path(directory):
    """The module path of the go.mod in directory, or None."""
    try:
        with open(os.path.join(directory, "go.mod"), encoding="utf-8") as f:
            for line in f:
                words = line.split()
                if words[:1] == ["module"] and len(words) > 1:
                    return words[1]
    except OSError:
        return None
    return None


def whole_module(pattern, cwd):
    """True when a package pattern names every package of a godi module:
    `all`, a module's import path followed by /..., or ./... at a module root."""
    if pattern == "all" or WHOLE_MODULE_PATTERN.fullmatch(pattern):
        return True
    path = module_path(cwd)
    return path is not None and GODI_MODULE.match(path) is not None and pattern in ("./...", "...")


def judge_make(words, cwd):
    targets, dry, i = [], False, 1
    while i < len(words):
        w = words[i]
        i += 1
        if w == "--":
            targets.extend(x for x in words[i:] if "=" not in x)
            break
        if w.startswith("--"):
            name, eq, _ = w.partition("=")
            dry = dry or name in MAKE_DRY_RUN
            if name in MAKE_OPTIONAL_NUMBER:
                if not eq and i < len(words) and re.fullmatch(r"[0-9.]+", words[i]):
                    i += 1
            elif name in MAKE_VALUE_OPTIONS and not eq:
                i += 1
            continue
        if w.startswith("-") and len(w) > 1:
            for j, letter in enumerate(w[1:], start=1):
                if letter in "nqt":
                    dry = True
                if letter in "CfIoW":
                    if j == len(w) - 1:
                        i += 1
                    break
                if letter in "jlO":
                    if letter in "jl" and j == len(w) - 1 and i < len(words) and re.fullmatch(r"[0-9.]+", words[i]):
                        i += 1
                    break
            continue
        if "=" in w:
            continue
        targets.append(w)
    if dry:
        return
    gates = [t for t in targets if t in FULL_GATE_TARGETS]
    if gates:
        refuse(f"godi hook: `make {gates[0]}` runs over every module, which CI does on every push. {GATE_ADVICE}")


def judge_go_test(words, cwd):
    i = 1
    while i < len(words) and words[i].startswith("-"):
        if words[i] == "-C" and i + 1 < len(words):
            cwd = os.path.join(cwd, words[i + 1])
            i += 1
        elif words[i].startswith("-C="):
            cwd = os.path.join(cwd, words[i][3:])
        i += 1
    if words[i : i + 1] != ["test"]:
        return
    rest, race, packages, j = words[i + 1 :], False, [], 0
    while j < len(rest):
        a = rest[j]
        j += 1
        if a in ("-args", "--args"):
            break
        if a.startswith("-"):
            name, eq, value = a.lstrip("-").partition("=")
            if name == "race":
                race = not eq or value.lower() in ("1", "t", "true")
            if not eq and name in GO_TEST_VALUE_FLAGS:
                j += 1
            continue
        packages.append(a)
    if any(whole_module(p, cwd) for p in packages):
        refuse(f"godi hook: `go test` over a whole module is the full suite, which CI runs on every push. {GATE_ADVICE}")
    if race and (len(packages) > 1 or any("..." in p for p in packages)):
        refuse("godi hook: `go test -race` across several packages costs what the full gate costs (-race made one "
               "package's tests 21x slower in a sibling project, 23 s against 491 s, 2026-10-15); CI runs the race "
               f"tests. Run the tests without -race, or -race on one package with -run. {GATE_ADVICE}")


def judge_golangci(words, cwd):
    args = words[1:]
    for a in args:
        name, eq, value = a.partition("=")
        if name == "--allow-parallel-runners" and (not eq or value.lower() not in ("false", "f", "0")):
            refuse("godi hook: golangci-lint takes --allow-serial-runners, never --allow-parallel-runners: its "
                   "machine-wide lock keeps several sessions' lint runs from starving the machine together.")
    run_at, k = None, 0
    while k < len(args):
        if not args[k].startswith("-"):
            run_at = k
            break
        if "=" not in args[k] and args[k] in GOLANGCI_VALUE_FLAGS:
            k += 1
        k += 1
    if run_at is None or args[run_at] != "run":
        return
    rest, packages, j = args[run_at + 1 :], [], 0
    while j < len(rest):
        a = rest[j]
        j += 1
        if a.startswith("-"):
            if "=" not in a and a in GOLANGCI_VALUE_FLAGS:
                j += 1
            continue
        packages.append(a)
    if not packages or any(whole_module(p, cwd) for p in packages):
        refuse("godi hook: golangci-lint over a whole module is part of the full gate, which CI runs on every "
               "push (a whole-module run took 52 s and 2.6 GiB in a sibling project). Lint the changed packages: "
               f"`golangci-lint run --allow-serial-runners ./<dir>`. {GATE_ADVICE}")
    files = [p for p in packages if p.endswith(".go")]
    if files:
        refuse(f"godi hook: lint packages, never files: golangci-lint on {files[0]} type-checks it without the rest "
               "of its package and reports bogus `undefined` errors. Name its directory: "
               "`golangci-lint run --allow-serial-runners ./<dir>`.")


def judge_gate(words, cwd):
    program = os.path.basename(words[0])
    if program == "for-each-module.sh":
        inner, _ = unwrap(words[1:])
        if not inner:
            return
        inner_program = os.path.basename(inner[0])
        if (inner_program == "go" and "test" in inner[1:]) or inner_program.startswith("golangci-lint"):
            refuse(f"godi hook: scripts/for-each-module.sh runs `{' '.join(inner[:2])}` in every module, which "
                   f"CI does on every push. {GATE_ADVICE}")
        return
    if program in MAKE_PROGRAMS:
        judge_make(words, cwd)
    elif program == "go":
        judge_go_test(words, cwd)
    elif program.startswith("golangci-lint"):
        judge_golangci(words, cwd)


def main_checkout(repo):
    """The main checkout of the repository repo is in, or None when git cannot
    say: the parent of the common git directory every worktree shares."""
    env = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
    try:
        run = subprocess.run(["git", "-C", repo, "rev-parse", "--path-format=absolute", "--git-common-dir"],
                             capture_output=True, text=True, timeout=5, env=env, check=False)
    except (OSError, subprocess.SubprocessError):
        return None
    common = run.stdout.strip()
    if run.returncode != 0 or os.path.basename(common) != ".git":
        return None
    return os.path.dirname(common)


def worktree_home(path):
    """True when path is inside some .claude/worktrees/ or a Claude scratchpad:
    the rule where git cannot name the main checkout."""
    parts = path.split(os.sep)
    for i in range(len(parts) - 2):
        if parts[i] == ".claude" and parts[i + 1] == "worktrees" and parts[i + 2]:
            return True
    return bool(SCRATCHPAD.fullmatch(path))


def judge_worktree_add(words, cwd):
    if os.path.basename(words[0]) != "git":
        return
    rest, repo = words[1:], cwd
    while rest and rest[0].startswith("-"):
        opt = rest.pop(0)
        if opt == "-C" and rest:
            repo = os.path.join(repo, os.path.expanduser(rest.pop(0)))
        elif opt in ("-c", "--git-dir", "--work-tree", "--namespace") and rest:
            rest.pop(0)
    if rest[:2] != ["worktree", "add"]:
        return
    rest, path = rest[2:], None
    while rest:
        a = rest.pop(0)
        if a == "--":
            path = rest[0] if rest else None
            break
        if a in WORKTREE_VALUE_OPTIONS:
            rest = rest[1:]
        elif not a.startswith("-"):
            path = a
            break
    if not path:
        return
    lexical = os.path.normpath(os.path.join(repo, os.path.expanduser(path)))
    if SCRATCHPAD.fullmatch(lexical) or SCRATCHPAD.fullmatch(os.path.realpath(lexical)):
        return
    main = main_checkout(repo)
    if main is None:
        if worktree_home(lexical) or worktree_home(os.path.realpath(lexical)):
            return
        home = "the main checkout's .claude/worktrees"
    else:
        home = os.path.join(main, ".claude", "worktrees")
        if os.path.realpath(os.path.dirname(lexical)) == os.path.realpath(home):
            return
    refuse(f"godi hook: {path} is not in {home}/. Worktrees live directly under the main checkout's "
           ".claude/worktrees/ (or in a Claude scratchpad), never beside the repository, inside another "
           f"worktree or in /tmp: `git worktree add {home}/<name> -b <branch> origin/main`.")


def judge(payload):
    if os.environ.get("CI"):
        return
    tool_input = payload.get("tool_input")
    command = tool_input.get("command") if isinstance(tool_input, dict) else None
    cwd = payload.get("cwd")
    if not isinstance(command, str) or not isinstance(cwd, str):
        return
    full_checks = os.environ.get("CLAUDE_FULL_CHECKS") == "1"
    for words, here in commands(command, cwd):
        try:
            judge_worktree_add(words, here)
            if not full_checks:
                judge_gate(words, here)
        except Refused:
            raise
        except Exception:  # noqa: BLE001 - a command it cannot judge passes
            continue


def main():
    try:
        payload = json.load(sys.stdin)
        if isinstance(payload, dict):
            judge(payload)
    except Refused as refusal:
        print(refusal, file=sys.stderr)
        sys.exit(2)
    except Exception:  # noqa: BLE001 - what cannot be parsed is let through
        pass
    sys.exit(0)


if __name__ == "__main__":
    main()
