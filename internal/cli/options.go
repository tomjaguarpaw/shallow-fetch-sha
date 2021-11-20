package cli

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/memory"
	log "github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
)

type StorageMode string

const (
	FileSystemStorageMode StorageMode = "fs"
	MemoryStorageMode     StorageMode = "mem"
)

var (
	regHex = regexp.MustCompile("^[0-9a-fA-F]+$")
)

type Options struct {
	Repo         string
	SHA          string
	Directory    string
	RemoveDotGit bool
	BasicAuth    *BasicAuthOptions
	SSHAuth      *SSHAuthOptions

	storage  storage.Storer
	worktree billy.Filesystem
}

type SSHAuthOptions struct {
	PEMPath    string
	Passphrase string
}

type BasicAuthOptions struct {
	Username string
	Password string
}

func (opts *Options) Auth() (transport.AuthMethod, error) {
	if opts.SSHAuth != nil {
		// default user to 'git'
		user := "git"

		// if different user specified in ssh url
		pieces := strings.Split(opts.Repo, ":")
		if len(pieces) == 2 {
			if parsed, err := url.Parse(pieces[0]); err != nil {
				parsedUser := parsed.User.Username()
				if parsedUser != "" {
					user = parsedUser
				}
			}
		}

		return ssh.NewPublicKeysFromFile(user, opts.SSHAuth.PEMPath, opts.SSHAuth.Passphrase)
	}

	if opts.BasicAuth != nil {
		user := opts.BasicAuth.Username
		if user == "" {
			// when using a token, username doesn't matter, but it can't be empty
			user = "token"
		}

		return &http.BasicAuth{
			Username: opts.BasicAuth.Username,
			Password: opts.BasicAuth.Password,
		}, nil
	}

	return nil, nil
}

func invalid(key, msg string) error {
	return fmt.Errorf("%q is invalid: %s", key, msg)
}

func (opts *Options) Validate() error {
	if opts.Repo == "" {
		return invalid("repo", "it is required")
	}

	if len(opts.SHA) != 40 || !regHex.MatchString(opts.SHA) {
		return invalid("sha", "must be full 40 hexadecimal character SHA1")
	}

	if opts.BasicAuth != nil && opts.SSHAuth != nil {
		return errors.New("cannot specify both basic auth and ssh auth options")
	}

	if opts.BasicAuth != nil {
		if opts.BasicAuth.Username == "" {
			return invalid("username", "required if password specified (if using token, set username to \"token\")")
		}

		if opts.BasicAuth.Password == "" {
			return invalid("password", "required if username specified")
		}
	}

	if opts.SSHAuth != nil {
		if opts.SSHAuth.PEMPath == "" {
			return invalid("key-path", "required if ssh options set")
		}
	}

	if opts.worktree == nil || opts.storage == nil {
		return errors.New("filesystem storage not initalized")
	}

	return nil
}

func (opts *Options) BindArgs(args []string) error {
	if len(args) != 2 {
		return errors.New("missing arguments: must specify both repo and sha arguments")
	}
	opts.Repo = args[0]
	opts.SHA = args[1]
	return nil
}

func (opts *Options) BindFlags(flags *flag.FlagSet) error {
	dir, err := flags.GetString("directory")
	if err != nil {
		return err
	}
	opts.Directory = dir

	username, err := flags.GetString("username")
	if err != nil {
		return err
	}
	if username != "" {
		if opts.BasicAuth == nil {
			opts.BasicAuth = &BasicAuthOptions{}
		}
		opts.BasicAuth.Username = username
	}

	password, err := flags.GetString("password")
	if err != nil {
		return err
	}
	if password != "" {
		if opts.BasicAuth == nil {
			opts.BasicAuth = &BasicAuthOptions{}
		}
		opts.BasicAuth.Password = password
	}

	keyPath, err := flags.GetString("key-path")
	if err != nil {
		return err
	}
	if keyPath != "" {
		if opts.SSHAuth == nil {
			opts.SSHAuth = &SSHAuthOptions{}
		}
		opts.SSHAuth.PEMPath = keyPath
	}

	keyPhrase, err := flags.GetString("key-passphrase")
	if err != nil {
		return err
	}
	if keyPhrase != "" {
		if opts.SSHAuth == nil {
			opts.SSHAuth = &SSHAuthOptions{}
		}
		opts.SSHAuth.Passphrase = keyPhrase
	}

	rmDotGit, err := flags.GetBool("rm-dotgit")
	if err != nil {
		return err
	}
	opts.RemoveDotGit = rmDotGit

	return nil
}

func (opts *Options) SetStorageMode(mode StorageMode) error {
	if opts.Directory == "" {
		return errors.New("must initalize directory before setting storage mode")
	}

	log.WithFields(log.Fields{
		"storagemode": mode,
	}).Debugln("initalizing working tree and storage")

	switch mode {
	case FileSystemStorageMode:
		absDir, err := filepath.Abs(opts.Directory)
		if err != nil {
			return fmt.Errorf("invalid directory: %s", err)
		}

		wt := osfs.New(absDir)

		dotGit, err := wt.Chroot(git.GitDirName)
		if err != nil {
			return err
		}

		opts.worktree = wt
		opts.storage = filesystem.NewStorage(dotGit, cache.NewObjectLRUDefault())

		return nil
	case MemoryStorageMode:
		opts.worktree = memfs.New()
		opts.storage = memory.NewStorage()

		return nil
	default:
		return fmt.Errorf("%q is an invalid storage mode", mode)
	}
}

func (opts *Options) GetWorkTree() billy.Filesystem {
	return opts.worktree
}

func (opts *Options) GetStorage() storage.Storer {
	return opts.storage
}
