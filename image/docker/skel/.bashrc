# Hyperwake dev image shell.

[[ $- != *i* ]] && return

HISTCONTROL=ignoreboth
HISTSIZE=10000
HISTFILESIZE=20000
shopt -s histappend checkwinsize

# A prompt that names the machine, because a remote shell that looks like your
# laptop's shell is how people run destructive commands in the wrong place.
PS1='\[\033[38;5;179m\]\u@hyperwake\[\033[0m\]:\[\033[38;5;223m\]\w\[\033[0m\]\$ '

alias ll='ls -alF --color=auto'
alias la='ls -A --color=auto'
alias ls='ls --color=auto'
alias grep='grep --color=auto'

export EDITOR=vim
export PATH="$HOME/.local/bin:$PATH"
