package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"github.com/hanwen/go-fuse/fuse"
	"github.com/hanwen/go-fuse/fuse/nodefs"
	"github.com/hanwen/go-fuse/fuse/pathfs"
	"github.com/hashicorp/golang-lru"
	"github.com/jonas747/discordgo"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type DiscordFS struct {
	pathfs.FileSystem
	nfs *pathfs.PathNodeFs

	cache *lru.Cache

	Session   *discordgo.Session
	Guild     string
	LastFetch *FileDesc
}

func NewFS(session *discordgo.Session, guild string) *pathfs.PathNodeFs {
	dfs := &DiscordFS{
		FileSystem: pathfs.NewDefaultFileSystem(),
		Session:    session,
		Guild:      guild,
	}
	cache, err := lru.New(10)
	if err != nil {
		log.Println("FAiled setting up cachce", err)
	}
	dfs.cache = cache

	session.AddHandler(dfs.OnReady)
	session.AddHandler(dfs.OnServerJoin)
	session.AddHandler(dfs.OnMessageCreate)
	session.AddHandler(dfs.OnMessageRemove)
	session.AddHandler(dfs.OnMessageEdit)
	session.AddHandler(dfs.OnChannelEdit)

	nfs := pathfs.NewPathNodeFs(pathfs.NewLockingFileSystem(dfs), nil)
	nfs.SetDebug(true)
	dfs.nfs = nfs
	return nfs
}

func (fs *DiscordFS) Mount() {
	server, _, err := nodefs.MountRoot(flag.Arg(2), fs.nfs.Root(), nil)
	if err != nil {
		log.Fatalf("Mount fail: %v\n", err)
	}
	log.Println("Serving")
	server.Serve()
}

func (fs *DiscordFS) GetAttr(name string, context *fuse.Context) (*fuse.Attr, fuse.Status) {
	log.Println("GETATTR", name)
	if name == "" {
		return &fuse.Attr{
			Mode: fuse.S_IFDIR | 0755,
		}, fuse.OK
	}

	fileDesc, err := fs.GetFileDesc(name)
	if err != nil {
		if err == ErrFileNotFound {
			log.Println("Not found...")
			return nil, fuse.ENOENT
		}
		log.Println("Failed getting child", err)
		return nil, fuse.EBADF
	}

	attr := &fuse.Attr{}
	fileDesc.GetAttr(attr)

	return attr, fuse.OK
}

func (fs *DiscordFS) OpenDir(name string, context *fuse.Context) (c []fuse.DirEntry, code fuse.Status) {
	log.Println("OPENDIR", name)

	fileDesc, err := fs.GetFileDesc(name)
	if err != nil {
		log.Println("Failed getting filedesc", err)
		return nil, fuse.EIO
	}

	dir, err := fileDesc.GetDirEntries()
	if err != nil {
		log.Println("Failed getting dir entries")
		return nil, fuse.EIO
	}

	for _, file := range dir {
		mode := fuse.S_IFREG
		if file.IsDir {
			mode = fuse.S_IFDIR
		}
		c = append(c, fuse.DirEntry{
			Mode: uint32(mode),
			Name: file.Path,
		})
	}
	code = fuse.OK
	return
}

func (fs *DiscordFS) Open(name string, flags uint32, context *fuse.Context) (file nodefs.File, code fuse.Status) {
	log.Println("OPEN", name, flags)

	fileDesc, err := fs.GetFileDesc(name)
	if err != nil {
		if err == ErrFileNotFound {
			return nil, fuse.ENOENT
		}
		log.Println("Failed opening file", err)
		return nil, fuse.EIO
	}

	return fileDesc, fuse.OK
}

func (fs *DiscordFS) Create(name string, flags uint32, mode uint32, context *fuse.Context) (file nodefs.File, code fuse.Status) {
	log.Println("CREATE", name, flags, mode)

	root, err := fs.GetRoot()
	if err != nil {
		log.Println("Failed getting root", err)
		return nil, fuse.EIO
	}

	fileDesc, err := root.GetChild(name)
	if err != nil {
		if err == ErrFileNotFound {
			parent, err := fs.GetFileParent(name)
			if err != nil {
				log.Println("failed getting parent", err)
				return nil, fuse.EIO
			}

			handle, _, err := fs.AllocateFileData(name, fs.Guild, []byte{}, 0)
			if err != nil {
				log.Println("Failed allocating file data", err)
				return nil, fuse.EIO
			}

			_, fileName := filepath.Split(name)
			fileDesc = &FileDesc{
				FS:            fs,
				Path:          name,
				Name:          fileName,
				DataStart:     handle,
				DataCapacity:  1,
				DataMsgCount:  1,
				DataChannelID: fs.Guild,
			}

			err = parent.AddChild(fileDesc)
			if err != nil {
				log.Println("FAiled updating parent", err)
				return nil, fuse.EIO
			}
			code = parent.Flush()
			if code != fuse.OK {
				return nil, code
			}
		} else {
			log.Println("Failed creating file, failed finding existing file", err)
			return nil, fuse.EIO
		}
	}

	return fileDesc, fuse.OK
}

func (fs *DiscordFS) Mkdir(name string, mode uint32, context *fuse.Context) fuse.Status {
	log.Println("MKDIR", name, mode)

	root, err := fs.GetRoot()
	if err != nil {
		log.Println("Failed getting root", err)
		return fuse.EBADF
	}

	pathRemoved := name

	var parent *FileDesc
	split := strings.Split(name, "/")
	if len(split) < 2 {
		parent = root
	} else {
		exludedNew := strings.Join(split[:len(split)-1], "/")
		p, err := root.GetChild(exludedNew)
		if err != nil {
			log.Println("Failed getting child", err)
			return fuse.EBADF
		}

		newSplit := strings.Split(name, "/")
		pathRemoved = newSplit[len(newSplit)-1]

		parent = p
	}

	handle, _, err := fs.AllocateFileData(name, fs.Guild, []byte("[]"), 2)
	if err != nil {
		log.Println("Failed allocating data", err)
		return fuse.EIO
	}

	curEntries, err := parent.GetDirEntries()
	if err != nil {
		log.Println("Failed receiving entries", err)
		return fuse.EIO
	}

	desc := &FileDesc{
		FS:            fs,
		Path:          name,
		Name:          pathRemoved,
		IsDir:         true,
		DataStart:     handle,
		DataChannelID: fs.Guild,
		DataMsgCount:  1,
		DataCapacity:  1,
	}

	curEntries = append(curEntries, desc)
	encoded, err := json.Marshal(curEntries)
	if err != nil {
		return fuse.EIO
	}

	parent.Cache = encoded
	return parent.Flush()
}

func (fs *DiscordFS) Rmdir(name string, context *fuse.Context) (code fuse.Status) {
	log.Println("RMDIR", name)
	err := fs.Delete(name)
	if err != nil {
		log.Println("Failed deleting", err)
		return fuse.EIO
	}
	return fuse.OK
}

func (fs *DiscordFS) Unlink(name string, context *fuse.Context) (code fuse.Status) {
	log.Println("UNLINK", name)
	err := fs.Delete(name)
	if err != nil {
		log.Println("Failed deleting", err)
		return fuse.EIO
	}
	return fuse.OK
}

func (fs *DiscordFS) Delete(name string) error {
	parent, err := fs.GetFileParent(name)
	if err != nil {
		return err
	}

	entries, err := parent.GetDirEntries()
	if err != nil {
		return err
	}

	for k, entry := range entries {
		if entry.Path == name {
			// remove
			entries = append(entries[:k], entries[k+1:]...)
			serialized, err := json.Marshal(entries)
			if err != nil {
				return err
			}
			parent.Cache = serialized
			parent.Flush()
			return nil
		}
	}
	return ErrFileNotFound
}

func (fs *DiscordFS) Rename(oldName string, newName string, context *fuse.Context) (code fuse.Status) {
	log.Println("RENAME", oldName, newName)
	parent, err := fs.GetFileParent(oldName)
	if err != nil {
		log.Println("Failed getting parent", err)
		return fuse.EIO
	}

	var target *FileDesc

	entries, err := parent.GetDirEntries()
	for k, entry := range entries {
		if entry.Path == newName {
			// Remove
			entries = append(entries[:k], entries[k+1:]...)
		} else if entry.Path == oldName {
			target = entry
		}
	}

	if target == nil {
		log.Println("Failed to find target")
		return fuse.ENOENT
	}
	_, newFName := filepath.Split(newName) // Take out the full path
	target.Name = newFName
	target.Path = newName

	serialized, err := json.Marshal(entries)
	if err != nil {
		log.Println("Failed serializing entries", err)
		return fuse.EBADF
	}
	parent.Cache = serialized
	code = parent.Flush()
	if code != fuse.OK {
		return code
	}

	if target.IsDir {
		childEntries, err := target.GetDirEntries()
		if err != nil {
			log.Println("Error getting child entries", err)
			return fuse.EIO
		}

		for _, entry := range childEntries {
			err = entry.UpdatePath(oldName, newName)
			if err != nil {
				log.Println("Failed updating child path, file will be lost")
			}
		}

		serialized, err := json.Marshal(childEntries)
		if err != nil {
			log.Println("Failed serializing child entries")
			return fuse.EBADF
		}

		target.Cache = serialized
		return target.Flush()
	}

	return fuse.OK

	// fileDesc, err := fs.GetFileDesc(oldName)
	// if err != nil {
	// 	log.Println("Failed getting filedesc", err)
	// 	return fuse.EIO
	// }
}

func (fs *DiscordFS) GetFileParent(name string) (*FileDesc, error) {
	parentDir, _ := filepath.Split(name)
	return fs.GetFileDesc(parentDir)
}

func (fs *DiscordFS) AllocateFileData(name, channel string, data []byte, size int) (start string, count int, err error) {
	// Handle
	msg, err := fs.Session.ChannelMessageSend(fs.Guild, name+" Handle")
	if err != nil {
		return
	}
	start = msg.ID

	buf := bytes.NewBuffer(data)
	for {
		// data
		part := make([]byte, BYTES_PER_MSG)
		n, err := buf.Read(part)
		stop := false
		if err != nil {
			if err == io.EOF {
				stop = true
			} else {
				return "", 0, err
			}
		}
		count++
		_, err = fs.Session.ChannelMessageSend(channel, "f"+string(part[:n]))
		if err != nil {
			return "", 0, err
		}
		if n < BYTES_PER_MSG-1 || stop {
			break
		}
	}

	return
}

func (fs *DiscordFS) GetFileDesc(name string) (*FileDesc, error) {
	if c, ok := fs.cache.Get(name); c != nil && ok {
		return c.(*FileDesc), nil
	}

	root, err := fs.GetRoot()
	if err != nil {
		return nil, err
	}
	ret, err := root.GetChild(name)
	if err == nil {
		fs.cache.ContainsOrAdd(name, ret)
	}
	return ret, err
}

func (fs *DiscordFS) GetRoot() (desc *FileDesc, err error) {
	// Header is stored in default channel topic
	channel, err := fs.Session.State.Channel(fs.Guild)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal([]byte(channel.Topic), &desc)
	if err != nil {
		return
	}
	desc.FS = fs
	return
}

func (fs *DiscordFS) OnReady(s *discordgo.Session, r *discordgo.Ready) {
	// guild, err := fs.Session.State.Guild(fs.Guild)
	// if err != nil {
	// 	return
	// }
	// if guild.Unavailable != nil && !*guild.Unavailable {
	// 	err := fs.Initialize()
	// 	if err != nil {
	// 		log.Println("Error intializing dfs", err)
	// 		os.Exit(0)
	// 		return
	// 	}
	// }
}

func (fs *DiscordFS) OnServerJoin(s *discordgo.Session, r *discordgo.GuildCreate) {
	if r.Guild.Unavailable != nil && !*r.Guild.Unavailable && r.Guild.ID == fs.Guild {
		err := fs.Initialize(r.Guild)
		if err != nil {
			log.Println("Error intiazling dfs", err)
			os.Exit(0)
			return
		}
		go fs.Mount()
		return
	}
}

// Initializes the the fs, creates the general topic (root header) if needed
func (fs *DiscordFS) Initialize(guild *discordgo.Guild) error {
	log.Println("Initializing")
	var channel *discordgo.Channel
	for _, v := range guild.Channels {
		if v.ID == fs.Guild {
			channel = v
			break
		}
	}
	if channel == nil {
		return errors.New("Channel not found in guild")
	}

	var header *FileDesc
	err := json.Unmarshal([]byte(channel.Topic), &header)
	if err == nil {
		return nil
	}

	handle, _, err := fs.AllocateFileData("/", fs.Guild, []byte("[]"), 2)
	if err != nil {
		return err
	}

	rootDesc := &FileDesc{
		IsDir:         true,
		IsRoot:        true,
		DataStart:     handle,
		DataChannelID: fs.Guild,
		DataMsgCount:  1,
		DataCapacity:  1,
	}

	encoded, err := json.Marshal(rootDesc)
	if err != nil {
		return err
	}

	_, err = fs.Session.ChannelEdit(fs.Guild, channel.Name, string(encoded), 0, 0, 8000)
	return err
}

func (fs *DiscordFS) WriteRootDesc(desc *FileDesc) error {
	encoded, err := json.Marshal(desc)
	if err != nil {
		return err
	}

	channel, err := fs.Session.State.Channel(fs.Guild)
	if err != nil {
		return err
	}

	_, err = fs.Session.ChannelEdit(fs.Guild, channel.Name, string(encoded), 0, 0, 8000)
	return err
}
