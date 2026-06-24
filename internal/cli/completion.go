package cli

import "fmt"

// Shell completion scripts. These complete the flags defined in Run() (cli.go)
// plus the enum value for --backend and paths for --dir/--instructions.
//
// NOTE: keep the flag list here in sync with the flag definitions in Run().

const bashCompletion = `# bash completion for departai
_departai() {
    local cur prev
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    case "$prev" in
        --backend)
            COMPREPLY=( $(compgen -W "claude codex" -- "$cur") )
            return
            ;;
        --dir|--instructions)
            COMPREPLY=( $(compgen -f -- "$cur") )
            return
            ;;
    esac

    if [[ "$cur" == -* ]]; then
        COMPREPLY=( $(compgen -W "--dir --instructions --max-turns --max-turn-duration --log-window --max-retries --model --backend --version --verbose --help" -- "$cur") )
    fi
}
complete -F _departai departai
`

const zshCompletion = `#compdef departai
# zsh completion for departai
_departai() {
    _arguments -s \
        '--dir[Working directory for agents]:directory:_files -/' \
        '--instructions[Custom base instructions file]:file:_files' \
        '--max-turns[Max agent turns (0 = unlimited)]:number:' \
        '--max-turn-duration[Per-turn wall-clock budget (e.g. 15m)]:duration:' \
        '--log-window[Inject only the last N turns]:number:' \
        '--max-retries[Retries on transient backend failure]:number:' \
        '--model[Model to use]:model:' \
        '--backend[Agent backend]:backend:(claude codex)' \
        '--version[Print version and exit]' \
        '--verbose[More detailed output]'
}
_departai "$@"
`

const fishCompletion = `# fish completion for departai
complete -c departai -f
complete -c departai -l dir -d 'Working directory for agents' -r -a '(__fish_complete_directories)'
complete -c departai -l instructions -d 'Custom base instructions file' -r
complete -c departai -l max-turns -d 'Max agent turns (0 = unlimited)' -x
complete -c departai -l max-turn-duration -d 'Per-turn wall-clock budget (e.g. 15m)' -x
complete -c departai -l log-window -d 'Inject only the last N turns' -x
complete -c departai -l max-retries -d 'Retries on transient backend failure' -x
complete -c departai -l model -d 'Model to use' -x
complete -c departai -l backend -d 'Agent backend' -x -a 'claude codex'
complete -c departai -l version -d 'Print version and exit'
complete -c departai -l verbose -d 'More detailed output'
`

// completionScript returns the completion script for a supported shell.
func completionScript(shell string) (string, error) {
	switch shell {
	case "bash":
		return bashCompletion, nil
	case "zsh":
		return zshCompletion, nil
	case "fish":
		return fishCompletion, nil
	default:
		return "", fmt.Errorf("unsupported shell %q (supported: bash, zsh, fish)", shell)
	}
}

// runCompletion handles the `departai completion <shell>` subcommand: it prints
// the shell completion script to stdout. Intercepted in Run() before flag
// parsing so it never collides with a task prompt.
func runCompletion(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: departai completion <bash|zsh|fish>")
	}
	script, err := completionScript(args[0])
	if err != nil {
		return err
	}
	fmt.Print(script)
	return nil
}
