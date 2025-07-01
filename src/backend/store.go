package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blinklabs-io/gouroboros/cbor"
	"github.com/blinklabs-io/gouroboros/ledger"
)

type Store struct {
	immutable *ImmStore
	volatile  *VolStore

	// the store is frequently notified of tip changes
	//  if the tip is different, the immutable and volatile stores must be updated
	//   updating the immutable store involves rereading the last modified chunk, and reading and appending any new chunks
	//   updating the volatile store also involves rereading the last modified chunk, and reading new chunks
	loadedTip string
}

// keeps all secondary indices in memory, ignoring the primary indices for now
// this uses a huge amount of memory (~1GB), but still fits nicely in memory of modern computers
//
//	for comparisson: the default postgresql database created by cardano-db-sync is much larger (~500GB)
type ImmStore struct {
	dir    string
	chunks []*ImmChunk

	blockPtrs map[string]BlockPtr // TODO: are there more efficient keys than using some string encoding of the block hash?
	mu        sync.RWMutex
}

// volatile store
type VolStore struct {
	dir string

	// these chunks are sparse, so use a map instead of a list
	chunks map[uint32]*VolChunk

	latestChunk uint32
	blockPtrs   map[string]BlockPtr
	mu          sync.RWMutex
}

type ImmChunk struct {
	modTime          time.Time
	secondaryIndices []SecondaryIndexEntry
}

// store completely in memory
type VolChunk struct {
	modTime time.Time
	blocks  []ledger.Block
}

// pointer into the immutable db
type BlockPtr struct {
	// Chunk index
	I uint32

	// Secondary index
	J uint32
}

// See section 8.2.2 of https://ouroboros-consensus.cardano.intersectmbo.org/pdfs/report.pdf
type SecondaryIndexEntry struct {
	BlockOffset   uint64
	HeaderOffset  uint16
	HeaderSize    uint16
	Checksum      uint32
	BlockID       [32]byte // aka. header hash
	SlotOrEpochNo uint64
}

func LoadStore(dir string) (*Store, error) {
	imm, err := LoadImmStore(filepath.Join(dir, "immutable"))
	if err != nil {
		return nil, err
	}

	vol, err := LoadVolStore(filepath.Join(dir, "volatile"))
	if err != nil {
		return nil, err
	}

	loadedTip := vol.Tip()

	if loadedTip == "" {
		loadedTip = imm.Tip()
	}

	return &Store{
		imm,
		vol,
		loadedTip,
	}, nil
}

func LoadImmStore(dir string) (*ImmStore, error) {
	chunks := make([]*ImmChunk, 0, 1000)

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		id, err := extractChunkID(path)
		if err != nil {
			log.Printf("%v", err)
			return nil
		}

		ext := filepath.Ext(path)

		switch ext {
		case ".secondary":
			chunk, err := loadImmChunk(path)
			if err != nil {
				log.Printf("failed to read immutable chunk %d: %v", id, err)
				return nil
			}

			for len(chunks) < int(id+1) {
				chunks = append(chunks, nil)
			}

			chunks[id] = chunk

			// TODO: read primary index files
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	fmt.Printf("Loaded secondary indices of %d chunks\n", len(chunks))

	return &ImmStore{
		dir,
		chunks,
		nil, // filled on-demand
		sync.RWMutex{},
	}, nil
}

func LoadVolStore(dir string) (*VolStore, error) {
	chunks := map[uint32]*VolChunk{}

	latestChunk := uint32(0)

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		id, err := extractChunkID(path)
		if err != nil {
			log.Printf("%v", err)
			return nil
		}

		ext := filepath.Ext(path)

		switch ext {
		case ".dat":
			chunk, err := loadVolChunk(path)
			if err != nil {
				log.Printf("failed to read volatile chunk %d: %v", id, err)
				return nil
			}

			chunks[id] = chunk

			if id > latestChunk {
				latestChunk = id
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	fmt.Printf("Loaded %d volatile chunks\n", len(chunks))

	return &VolStore{
		dir,
		chunks,
		latestChunk,
		nil, // filled on-demand
		sync.RWMutex{},
	}, nil
}

func (s *ImmStore) Tip() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	n := len(s.chunks)

	if n == 0 {
		return ""
	}

	c := s.chunks[n-1]
	return c.Tip()
}

func (s *VolStore) Tip() string {
	s.mu.RLock()
	chunk, ok := s.chunks[s.latestChunk]
	s.mu.RUnlock()
	if !ok {
		return ""
	}

	return chunk.Tip()
}

func (s *Store) NotifyTip(tip string) {
	if s.loadedTip == tip {
		return
	}

	// tip is highly unlikely to be in immutable store, so only check volatile store here
	if s.volatile.has(tip) {
		s.loadedTip = tip
		return
	}

	s.immutable.sync()
	s.volatile.sync()

	s.loadedTip = tip
}

func (s *ImmStore) chunkFilePath(id int) string {
	return filepath.Join(s.dir, fmt.Sprintf("%05d.secondary", id))
}

func (s *ImmStore) latestChunkID() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latestChunkIDLocked()
}

// caller must hold at least a read lock on s.mu
func (s *ImmStore) latestChunkIDLocked() int {
	return len(s.chunks) - 1
}

func (s *ImmStore) sync() {
	s.syncLoadedBlocks()

	s.syncNewBlocks()
}

func (s *ImmStore) syncLoadedBlocks() {
	s.mu.Lock()
	chunkID := s.latestChunkIDLocked()
	chunk := s.chunks[chunkID]
	path := s.chunkFilePath(chunkID)

	f, err := os.Open(path)
	if err != nil {
		fmt.Printf("unable to open %s during syncing: %v", path, err)
	} else {
		defer f.Close()

		stat, err := f.Stat()
		if err != nil {
			fmt.Printf("unable to stat %s during syncing: %v", path, err)
		} else {
			if stat.ModTime().After(chunk.modTime) {
				// reload chunk
				reloadedChunk, err := loadImmChunk(path)
				if err != nil {
					fmt.Printf("unable to reload immutable chunk %s: %v", path, err)
				} else {
					if s.blockPtrs != nil {
						reloadedChunk.indexBlocks(s.blockPtrs, chunkID)
					}
					s.chunks[chunkID] = reloadedChunk
				}
			}
		}
	}
	s.mu.Unlock()
}

func (s *ImmStore) syncNewBlocks() {
	s.mu.Lock()
	for nextID := s.latestChunkIDLocked() + 1; true; nextID++ {
		nextPath := s.chunkFilePath(nextID)
		nextFile, err := os.Open(nextPath)
		if err != nil {
			if err != os.ErrNotExist && !strings.Contains(err.Error(), "no such file") {
				fmt.Printf("unable to read immutable chunk %s: %v", nextPath, err)
			}

			break
		}

		defer nextFile.Close()

		nextChunk, err := readImmChunk(nextFile)
		if err != nil {
			log.Printf("unable to read immutable chunk %s: %v", nextPath, err)
			break
		}

		if s.blockPtrs != nil {
			nextChunk.indexBlocks(s.blockPtrs, nextID)
		}

		s.chunks = append(s.chunks, nextChunk)
	}
	s.mu.Unlock()
}

func (s *VolStore) chunkFilePath(id int) string {
	return filepath.Join(s.dir, fmt.Sprintf("blocks-%04d.dat", id))
}

func (s *VolStore) latestChunkID() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latestChunkIDLocked()
}

// caller must hold at least a read lock on s.mu
func (s *VolStore) latestChunkIDLocked() int {
	latestChunkID := -1

	for id := range s.chunks {
		if int(id) > latestChunkID {
			latestChunkID = int(id)
		}
	}

	return latestChunkID
}

// first sync the existing files
func (s *VolStore) sync() {
	s.syncLoadedBlocks()

	s.syncNewBlocks()

	s.pruneOrphanedBlockPtrs()
}

func (s *VolStore) syncLoadedBlocks() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for chunkID, chunk := range s.chunks {
		path := s.chunkFilePath(int(chunkID))

		f, err := os.Open(path)
		if err != nil {
			if err == os.ErrNotExist {
				fmt.Printf("removing volatile chunk %s\n", path)

				delete(s.chunks, chunkID)
			} else {
				fmt.Printf("unable to open %s during syncing: %v\n", path, err)
			}
		} else {
			defer f.Close()

			stat, err := f.Stat()
			if err != nil {
				fmt.Printf("unable to stat %s during syncing: %v\n", path, err)
			} else {
				if stat.ModTime().After(chunk.modTime) {
					// reload chunk
					reloadedChunk, err := loadVolChunk(path)
					if err != nil {
						fmt.Printf("unable to reload volatile chunk %s: %v\n", path, err)
					} else {
						if s.blockPtrs != nil {
							reloadedChunk.indexBlocks(s.blockPtrs, int(chunkID))
						}

						s.chunks[chunkID] = reloadedChunk
					}
				}
			}
		}
	}
}

func (s *VolStore) syncNewBlocks() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for nextID := s.latestChunkIDLocked() + 1; true; nextID += 1 {
		nextPath := s.chunkFilePath(nextID)
		nextFile, err := os.Open(nextPath)
		if err != nil {
			if err != os.ErrNotExist && !strings.Contains(err.Error(), "no such file") {
				log.Printf("unable to read volatile chunk %s: %v", nextPath, err)
			}

			break
		}

		defer nextFile.Close()

		nextChunk, err := readVolChunk(nextFile)
		if err != nil {
			log.Printf("unable to read volatile chunk %s: %v", nextPath, err)
			break
		}

		if s.blockPtrs != nil {
			nextChunk.indexBlocks(s.blockPtrs, nextID)
		}

		s.chunks[uint32(nextID)] = nextChunk
	}
}

func (s *VolStore) pruneOrphanedBlockPtrs() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.blockPtrs == nil {
		return
	}

	for k, bp := range s.blockPtrs {
		if _, ok := s.chunks[bp.I]; !ok {
			delete(s.blockPtrs, k)
		}
	}
}

// TODO: return ledger.Block instead of its CBOR bytes as hex
func (s *Store) Block(blockID string) (ledger.Block, error) {
	// first look up in immutable store due more likely cache hit
	b, err := s.immutable.block(blockID)
	if err != nil {
		return nil, err
	}

	if b != nil {
		return b, nil
	}

	// now try looking up in volatile store
	b = s.volatile.block(blockID)

	return b, nil
}

func (s *ImmStore) has(blockID string) bool {
	s.mu.RLock()
	if s.blockPtrs == nil {
		s.mu.RUnlock()
		s.indexBlocks()
		s.mu.RLock()
	}
	defer s.mu.RUnlock()

	_, ok := s.blockPtrs[blockID]
	return ok
}

// blockID is its hex encoded hash
// returns the hex encoded bytes
// returns nil if not found
func (s *ImmStore) block(blockID string) (ledger.Block, error) {
	s.mu.RLock()
	if s.blockPtrs == nil {
		s.mu.RUnlock()
		s.indexBlocks()
		s.mu.RLock()
	}
	defer s.mu.RUnlock()

	ptr, ok := s.blockPtrs[blockID]
	if !ok {
		return nil, nil
	}

	chunk := s.chunks[ptr.I]
	secondaryIndex := chunk.secondaryIndices[ptr.J]

	file, err := os.Open(filepath.Join(s.dir, fmt.Sprintf("%05d.chunk", ptr.I)))
	if err != nil {
		return nil, err
	}

	defer file.Close()

	if _, err := file.Seek(int64(secondaryIndex.BlockOffset), 0); err != nil {
		return nil, err
	}

	isLast := int(ptr.J) == len(chunk.secondaryIndices)-1
	blockSize := 0

	if !isLast {
		blockSize = int(chunk.secondaryIndices[ptr.J+1].BlockOffset - secondaryIndex.BlockOffset)
	} else {
		stat, err := file.Stat()
		if err != nil {
			return nil, err
		}

		// max Size
		blockSize = int(uint64(stat.Size()) - secondaryIndex.BlockOffset)
	}

	bs := make([]byte, int(blockSize))

	n, err := file.Read(bs)
	if err != nil {
		return nil, err
	}

	if !isLast && n != int(blockSize) {
		return nil, fmt.Errorf("unexpected number of bytes read")
	}

	b, nDecoded, err := decodeWrappedBlock(bs)
	if err != nil {
		return nil, err
	}

	if nDecoded != n {
		log.Printf("decoded %d bytes, but block is only %d bytes", nDecoded, n)
	}

	return b, nil
}

func (s *VolStore) has(blockID string) bool {
	s.mu.RLock()
	if s.blockPtrs == nil {
		s.mu.RUnlock()
		s.indexBlocks()
		s.mu.RLock()
	}
	defer s.mu.RUnlock()

	_, ok := s.blockPtrs[blockID]
	return ok
}

// returns nil if not found
func (s *VolStore) block(blockID string) ledger.Block {
	s.mu.RLock()
	if s.blockPtrs == nil {
		s.mu.RUnlock()
		s.indexBlocks()
		s.mu.RLock()
	}
	defer s.mu.RUnlock()

	ptr, ok := s.blockPtrs[blockID]
	if !ok {
		return nil
	}

	chunk, ok := s.chunks[ptr.I]
	if !ok {
		return nil
	}

	var b ledger.Block
	if int(ptr.J) < len(chunk.blocks) {
		b = chunk.blocks[ptr.J]
	}
	return b
}

func (s *ImmStore) indexBlocks() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.blockPtrs = make(map[string]BlockPtr)

	for chunkID, chunk := range s.chunks {
		chunk.indexBlocks(s.blockPtrs, chunkID)
	}
}

func (c *ImmChunk) indexBlocks(ptrs map[string]BlockPtr, chunkID int) {
	for i, entry := range c.secondaryIndices {
		key := hex.EncodeToString(entry.BlockID[:])

		ptrs[key] = BlockPtr{uint32(chunkID), uint32(i)}
	}
}

func (s *VolStore) indexBlocks() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.blockPtrs = map[string]BlockPtr{}

	for chunkID, chunk := range s.chunks {
		chunk.indexBlocks(s.blockPtrs, int(chunkID))
	}
}

func (c *VolChunk) indexBlocks(ptrs map[string]BlockPtr, chunkID int) {
	for i, b := range c.blocks {
		hash := b.Header().Hash()
		key := hex.EncodeToString(hash[:])

		ptrs[key] = BlockPtr{uint32(chunkID), uint32(i)}
	}
}

// move this function outside, so it can also be used for the volatile part
func (s *Store) BlockTx(blockID string, txIndex int) (ledger.Transaction, error) {
	b, err := s.Block(blockID)
	if err != nil {
		return nil, err
	}

	if b == nil {
		return nil, fmt.Errorf("block %s not found", blockID)
	}

	txs := b.Transactions()

	if txIndex >= len(txs) {
		return nil, nil
	}

	if txIndex < 0 {
		return nil, fmt.Errorf("negative tx index %d", txIndex)
	}

	return txs[txIndex], nil
}

func (s *ImmStore) Status() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := len(s.chunks)
	lastChunk := s.chunks[n-1]
	nEntries := len(lastChunk.secondaryIndices)
	lastEntry := lastChunk.secondaryIndices[nEntries-1]

	return fmt.Sprintf("{\"slot\": %d, \"chunk\": %d}", lastEntry.SlotOrEpochNo, n), nil
}

func (c *ImmChunk) Tip() string {
	m := len(c.secondaryIndices)

	if m == 0 {
		return ""
	} else {
		return hex.EncodeToString(c.secondaryIndices[m-1].BlockID[:])
	}
}

func (c *VolChunk) Tip() string {
	m := len(c.blocks)

	if m == 0 {
		return ""
	} else {
		hash := c.blocks[m-1].Header().Hash()
		return hex.EncodeToString(hash[:])
	}
}

func loadImmChunk(path string) (*ImmChunk, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	return readImmChunk(file)
}

func readImmChunk(file *os.File) (*ImmChunk, error) {
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	indices := make([]SecondaryIndexEntry, 0)

	for true {
		var entry SecondaryIndexEntry

		// BigEndian has been verified to be correct thanks to trial-and-error
		err := binary.Read(file, binary.BigEndian, &entry)
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		indices = append(indices, entry)
	}

	return &ImmChunk{
		stat.ModTime(),
		indices,
	}, nil
}

func loadVolChunk(path string) (*VolChunk, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	return readVolChunk(file)
}

func readVolChunk(file *os.File) (*VolChunk, error) {
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	bs, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	blocks := make([]ledger.Block, 0)

	for len(bs) > 0 {
		block, n, err := decodeWrappedBlock(bs)
		if err != nil {
			log.Printf("failed to read block %d from %s: %v", len(blocks)+1, stat.Name(), err)
			break
		}

		blocks = append(blocks, block)

		bs = bs[n:]
	}

	return &VolChunk{stat.ModTime(), blocks}, nil
}

func nextCBORItem(bs []byte) ([]byte, []byte, error) {
	var v interface{}
	nRead, err := cbor.Decode(bs, &v)
	if err != nil {
		return nil, nil, err
	}

	sub := bs[0:nRead]
	rest := bs[nRead:]

	return sub, rest, nil
}

func extractChunkID(path string) (uint32, error) {
	// remove directories and file extension
	idStr, _, _ := strings.Cut(filepath.Base(path), ".")

	// remove optional "blocks-" prefixes
	_, maybeIDStr, ok := strings.Cut(idStr, "-")
	if ok {
		idStr = maybeIDStr
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse chunk id from %s", idStr)
	}

	return uint32(id), nil
}

func decodeWrappedBlock(bs []byte) (ledger.Block, int, error) {
	arrayHeader := bs[0]
	if arrayHeader != 0x82 {
		return nil, 0, fmt.Errorf("unexpected array header byte %d", arrayHeader)
	}

	blockType := bs[1]
	rest := bs[2:]

	b, n, err := decodeBlock(int(blockType), rest)

	return b, n + 2, err
}

func decodeBlock(blockType int, bs []byte) (ledger.Block, int, error) {
	switch blockType {
	case ledger.BlockTypeByronEbb:
		var b ledger.ByronEpochBoundaryBlock
		return decodeEraBlock[*ledger.ByronEpochBoundaryBlock](bs, &b)
	case ledger.BlockTypeByronMain:
		var b ledger.ByronMainBlock
		return decodeEraBlock[*ledger.ByronMainBlock](bs, &b)
	case ledger.BlockTypeShelley:
		var b ledger.ShelleyBlock
		return decodeEraBlock[*ledger.ShelleyBlock](bs, &b)
	case ledger.BlockTypeAllegra:
		var b ledger.AllegraBlock
		return decodeEraBlock[*ledger.AllegraBlock](bs, &b)
	case ledger.BlockTypeMary:
		var b ledger.MaryBlock
		return decodeEraBlock[*ledger.MaryBlock](bs, &b)
	case ledger.BlockTypeAlonzo:
		var b ledger.AlonzoBlock
		return decodeEraBlock[*ledger.AlonzoBlock](bs, &b)
	case ledger.BlockTypeBabbage:
		var b ledger.BabbageBlock
		return decodeEraBlock[*ledger.BabbageBlock](bs, &b)
	case ledger.BlockTypeConway:
		var b ledger.ConwayBlock
		return decodeEraBlock[*ledger.ConwayBlock](bs, &b)
	default:
		return nil, 0, fmt.Errorf("unhandled block type %d", blockType)
	}
}

type SettableCbor interface {
	SetCbor(bs []byte)
}

func decodeEraBlock[T SettableCbor](bs []byte, b T) (T, int, error) {
	n, err := cbor.Decode(bs, b)
	if err != nil {
		return b, 0, err
	}

	return b, n, nil
}
