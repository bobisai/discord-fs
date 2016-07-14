package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/hanwen/go-fuse/fuse"
	"github.com/hanwen/go-fuse/fuse/nodefs"
	"log"
	"math"
	"strings"
	"time"
)

const (
	BYTES_PER_MSG = 1999
)

var fileDataEncoder = base64.StdEncoding

type FileDesc struct {
	nodefs.File `json:"-"`
	FS          *DiscordFS `json:"-"`

	Name   string `json:"name,omitempty"`
	Path   string `json:"path"`
	IsRoot bool   `json:"is_root,omitemptyt"`
	IsDir  bool   `json:"is_dir,omitempty"`

	Size int `json:"size"`

	DataCapacity  int    `json:"capacity"` // How many messages is allocated, is alreays >= count
	DataStart     string `json:"start_id"`
	DataChannelID string `json:"channel_id"`
	DataMsgCount  int    `json:"count"`

	Dirty bool   `json:"-"` // True if the file changed, should be sent again on flush then
	Cache []byte `json:"-"` // cache
}

var (
	ErrFileTooLarge = errors.New("File is larger than 200KB")
	ErrNotDir       = errors.New("Not a directory")
	ErrFileNotFound = errors.New("File not found")
)

func (f *FileDesc) GetData() ([]byte, error) {
	// Will add support for this later
	if f.DataMsgCount > 100 {
		return nil, ErrFileTooLarge
	}

	if f.Cache != nil {
		return f.Cache, nil
	}

	msgs, err := f.FS.Session.ChannelMessages(f.DataChannelID, f.DataMsgCount, "", f.DataStart)
	if err != nil {
		return nil, err
	}
	if len(msgs) < 1 {
		return []byte{}, nil
	}
	data := make([]byte, 0)
	for i := len(msgs) - 1; i >= 0; i-- {
		data = append(data, []byte(msgs[i].Content[1:])...)
	}
	log.Println(string(data))
	f.Cache = data // cache the mafucka
	return data, nil
}

// Returns file entries in this folder
// Panics if f is not a folder
func (f *FileDesc) GetDirEntries() (entries []*FileDesc, err error) {
	if !f.IsDir {
		panic("Not a directory")
	}

	data, err := f.GetData()
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(data, &entries)
	if err != nil {
		return
	}

	for _, v := range entries {
		v.FS = f.FS
	}
	return
}

func (f *FileDesc) GetChild(path string) (*FileDesc, error) {
	if !f.IsDir {
		return nil, ErrNotDir
	}
	split := strings.SplitN(path, "/", 2)
	if split[0] == "" {
		return f, nil
	}

	entries, err := f.GetDirEntries()
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.Name == split[0] {
			if len(split) < 2 {
				return entry, nil
			} else {
				return entry.GetChild(split[1])
			}
		}
	}
	return nil, ErrFileNotFound
}

func (f *FileDesc) AddChild(desc *FileDesc) error {
	currentEntries, err := f.GetDirEntries()
	if err != nil {
		return err
	}

	currentEntries = append(currentEntries, desc)
	serialized, err := json.Marshal(currentEntries)
	if err != nil {
		return err
	}

	f.Cache = serialized
	return nil
}

func (f *FileDesc) WriteInode() {
	if f.IsRoot {
		err := f.FS.WriteRootDesc(f)
		if err != nil {
			log.Println("Error writing root desc", err)
		}
		return
	}
	parentDesc, err := f.FS.GetFileParent(f.Path)
	if err != nil {
		log.Println("Failed retrieving parent", err)
		return
	}

	entries, err := parentDesc.GetDirEntries()
	if err != nil {
		log.Println("Failed retrieveving entries", err)
		return
	}

	for k, v := range entries {
		if v.Path == f.Path {
			entries[k] = f
			break
		}
	}

	serialized, err := json.Marshal(entries)
	if err != nil {
		log.Println("Failed serializing entries", err)
		return
	}
	parentDesc.Cache = serialized
	parentDesc.Flush()

	//parentDesc := f.FS.GetFileParent(f.Path)
}

///////////////////////////
// Implement nodefs.File
///////////////////////////

// Called upon registering the filehandle in the inode.
func (f *FileDesc) SetInode(*nodefs.Inode) { log.Println("SET INODE") }

// The String method is for debug printing.
func (f *FileDesc) String() string { return f.Name }

// Wrappers around other File implementations, should return
// the inner file here.
func (f *FileDesc) InnerFile() nodefs.File { return f }

// Nondirectory filedata is encoded in base64 to be on the safe side
func (f *FileDesc) Read(dest []byte, off int64) (fuse.ReadResult, fuse.Status) {
	log.Println("READ", off, len(dest))
	data, err := f.GetData()
	if err != nil {
		return nil, fuse.EBADF
	}

	decoded := make([]byte, fileDataEncoder.DecodedLen(len(data)))
	n, err := fileDataEncoder.Decode(decoded, data)
	if err != nil {
		log.Println("Failed decoding data")
		return nil, fuse.EBADF
	}
	decoded = decoded[:n] // Baibai padding

	if off >= int64(len(decoded)) {
		return nil, fuse.EINVAL
	}

	toRead := int64(len(dest))
	if toRead+off >= int64(len(decoded)) {
		toRead = int64(len(decoded)) - off
		log.Println("Bigger than input")
	}

	copy(dest, decoded[off:off+toRead])
	rs := NewReadResult(dest, int(toRead))
	return rs, fuse.OK

	// offEncoded := int64(fileDataEncoder.EncodedLen(int(off)))

	// if offEncoded >= int64(len(data)) {
	// 	return nil, fuse.EINVAL
	// }

	// toRead := int64(fileDataEncoder.EncodedLen(len(dest)))
	// if toRead+offEncoded > int64(len(data)) {
	// 	toRead = int64(len(data)) - offEncoded
	// 	log.Println("Bigger than input")
	// }

	// toDecode := data[offEncoded : offEncoded+toRead]
	// n, err := fileDataEncoder.Decode(dest, toDecode)
	// if err != nil {
	// 	log.Println("Failed decoding data", err)
	// 	return nil, fuse.EBADF
	// }

	// realLen := n
	// log.Println("REAl len", realLen, len(toDecode), dest[:realLen])
	// rs := NewReadResult(dest[:realLen], realLen)
	// return rs, fuse.OK
}

func (f *FileDesc) Write(data []byte, off int64) (written uint32, code fuse.Status) {
	log.Println("WRITE", len(data), off)

	// We need the cache for this so load up the cache if needed
	if f.Cache == nil {
		_, err := f.GetData()
		if err != nil {
			log.Println("Failed loading data", err)
			return 0, fuse.EBADF
		}
	}

	decoded := make([]byte, fileDataEncoder.DecodedLen(len(f.Cache)))
	n, err := fileDataEncoder.Decode(decoded, f.Cache)
	if err != nil {
		log.Println("Failed decoding data", err)
		return 0, fuse.EBADF
	}
	decoded = decoded[:n] // strip away padding

	// encoded := make([]byte, fileDataEncoder.EncodedLen(len(data)))
	// base64.StdEncoding.Encode(encoded, data)

	// offEncoded := int64(base64.StdEncoding.EncodedLen(int(off)))

	if int64(len(data))+off > int64(len(decoded)) {
		//temp := f.Cache
		newBuf := make([]byte, off+int64(len(data)))
		//f.Cache =
		copy(newBuf, decoded)
		f.Size = len(newBuf)
		decoded = newBuf
		log.Println("Expanded buffer")
		// Since size changed we need to rewrite the filedesc
		f.WriteInode()
	}

	for i := int64(0); i < int64(len(data)); i++ {
		offsetI := i + off
		decoded[offsetI] = data[i]
	}

	encoded := make([]byte, fileDataEncoder.EncodedLen(len(decoded)))
	fileDataEncoder.Encode(encoded, decoded)
	f.Cache = encoded
	f.Dirty = true
	return uint32(len(data)), fuse.OK
}

// Flush is called for close() call on a file descriptor. In
// case of duplicated descriptor, it may be called more than
// once for a file.
func (f *FileDesc) Flush() fuse.Status {
	if !f.IsDir && !f.Dirty {
		log.Println("Not Dirty, no flush needed")
		return fuse.OK
	}

	log.Println("Need to flush", string(f.Cache))
	reqPerMsg := int(math.Ceil(float64(len(f.Cache)) / BYTES_PER_MSG))

	if reqPerMsg > f.DataMsgCount {
		log.Println("NEED TO RESIZE, REACCOLCATING FILE")
		// Resize yoooo
		start, count, err := f.FS.AllocateFileData(f.Path, f.FS.Guild, f.Cache, len(f.Cache))
		if err != nil {
			log.Println("Failed resizing")
			return fuse.EIO
		}

		f.DataStart = start
		f.DataMsgCount = count
		log.Println("New count", count, "Writing inode!", reqPerMsg)
		f.WriteInode()
		return fuse.OK
	}
	f.DataMsgCount = reqPerMsg

	msgs, err := f.FS.Session.ChannelMessages(f.DataChannelID, f.DataMsgCount, "", f.DataStart)
	if err != nil {
		log.Println("Error getting messages")
		return fuse.EIO
	}

	if len(msgs) < 1 {
		log.Println("Msgs is <1?")
		return fuse.EIO
	}

	_, err = f.FS.Session.ChannelMessageEdit(f.DataChannelID, msgs[0].ID, "f"+string(f.Cache))
	if err != nil {
		log.Println("Failed editing messages")
		return fuse.EIO
	}

	return fuse.OK
}

// This is called to before the file handle is forgotten. This
// method has no return value, so nothing can synchronizes on
// the call. Any cleanup that requires specific synchronization or
// could fail with I/O errors should happen in Flush instead.
func (f *FileDesc) Release()                           {}
func (f *FileDesc) Fsync(flags int) (code fuse.Status) { return fuse.OK }

// The methods below may be called on closed files, due to
// concurrency.  In that case, you should return EBADF.
func (f *FileDesc) Truncate(size uint64) fuse.Status {
	log.Println("TRUNCATE", size)
	return fuse.OK
}
func (f *FileDesc) GetAttr(out *fuse.Attr) fuse.Status {
	mode := fuse.S_IFREG
	if f.IsDir {
		mode = fuse.S_IFDIR
	}
	log.Println("F GETATTR", out)
	*out = fuse.Attr{
		Size: uint64(f.Size),
		Mode: uint32(mode | 0755),
	}
	return fuse.OK
}
func (f *FileDesc) Chown(uid uint32, gid uint32) fuse.Status               { return fuse.OK }
func (f *FileDesc) Chmod(perms uint32) fuse.Status                         { return fuse.OK }
func (f *FileDesc) Utimens(atime *time.Time, mtime *time.Time) fuse.Status { return fuse.OK }

func (f *FileDesc) Allocate(off uint64, size uint64, mode uint32) (code fuse.Status) {
	return fuse.OK
}

type ReadResult struct {
	buf  []byte
	read int
}

func NewReadResult(b []byte, read int) *ReadResult {
	// buf := make([]byte, len(b)) // Copy the data so we dont fuck with the underlying array
	// copy(buf, b)
	log.Println("RS", string(b))
	return &ReadResult{
		buf:  b,
		read: read,
	}
}

// Returns the raw bytes for the read, possibly using the
// passed buffer. The buffer should be larger than the return
// value from Size.
func (rs *ReadResult) Bytes(buf []byte) ([]byte, fuse.Status) {
	return rs.buf, fuse.OK
}

// Size returns how many bytes this return value takes at most.
func (rs *ReadResult) Size() int {
	return rs.read
}

// Done() is called after sending the data to the kernel.
func (rs *ReadResult) Done() { log.Println("DONE") }
