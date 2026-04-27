package ouroboros

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"sync"

	"lukechampine.com/blake3"
)

const (
	// Access Permissions (Bits 0-5)
	ReadOwn   uint32 = 1 << iota // 1
	ReadAll                      // 2
	WriteOwn                     // 4
	WriteAll                     // 8
	DeleteOwn                    // 16
	DeleteAll                    // 32

	// Action Flags (Bits 6-8)
	Defer
	Merge
	// Special Attributes (Bits 9-11)
	IsSuper
	PublicRead
	IsMigrated
	G11
	G12
	G13
	G14
	G15
	G16
	G17
	G18
	G19
	G20
	G21
	G22
	G23
	G24
	G25
	G26
	G27
	G28
	G29
	G30

	// System Flags (Bit 31)
	RingPhase uint32 = 1 << 31 // Antes 'GhostFlag'
)

const CelulaSize int64 = 64

var (
	ErrOutOfBounds   = errors.New("index out of bounds")
	ErrUnauthorized  = errors.New("unauthorized access")
	ErrInvalidConfig = errors.New("invalid database configuration")
	ErrInvalidDBFile = errors.New("invalid database file")
)

// Celula representa la estructura base
type Celula struct {
	Hash   [32]byte
	Salt   [16]byte
	Genoma uint32
	X      uint32
	Y      uint32
	Z      uint32
}

// toBytes serializa la Célula a un arreglo estricto de 64 bytes (LittleEndian)
func (c *Celula) toBytes() []byte {
	b := make([]byte, CelulaSize)
	copy(b[0:32], c.Hash[:])
	copy(b[32:48], c.Salt[:])
	binary.LittleEndian.PutUint32(b[48:52], c.Genoma)
	binary.LittleEndian.PutUint32(b[52:56], c.X)
	binary.LittleEndian.PutUint32(b[56:60], c.Y)
	binary.LittleEndian.PutUint32(b[60:64], c.Z)
	return b
}

// fromBytes deserializa desde un arreglo de bytes a la estructura Célula
func fromBytes(b []byte) Celula {
	var c Celula
	copy(c.Hash[:], b[0:32])
	copy(c.Salt[:], b[32:48])
	c.Genoma = binary.LittleEndian.Uint32(b[48:52])
	c.X = binary.LittleEndian.Uint32(b[52:56])
	c.Y = binary.LittleEndian.Uint32(b[56:60])
	c.Z = binary.LittleEndian.Uint32(b[60:64])
	return c
}

// Funciones auxiliares de Genoma
func getGhost(genoma uint32) bool {
	return (genoma & RingPhase) != 0
}

func setGhost(genoma uint32, phase bool) uint32 {
	if phase {
		return genoma | RingPhase
	}
	return genoma & (^RingPhase)
}

// OuroborosDB es el manejador del anillo
type OuroborosDB struct {
	mu         sync.RWMutex // Maneja el patrón SWMR
	file       *os.File
	cursor     uint32
	phase      bool
	maxRecords uint32
}

// Close libera el descriptor de archivo de la base de datos.
func (db *OuroborosDB) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.file == nil {
		return nil
	}

	err := db.file.Close()
	db.file = nil
	return err
}

// OpenOuroborosDB inicializa y recupera el estado de la base de datos
func OpenOuroborosDB(path string, maxRecords uint32) (*OuroborosDB, error) {
	if maxRecords == 0 {
		return nil, ErrInvalidConfig
	}

	// Se abre para lectura y escritura concurrente en disco
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}

	db := &OuroborosDB{
		file:       f,
		maxRecords: maxRecords,
	}

	wantedSize := int64(maxRecords) * CelulaSize

	// Recuperar estado (asegura tamaño para leer hasta maxRecords-1)
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if info.Size() > 0 && info.Size()%CelulaSize != 0 {
		_ = f.Close()
		return nil, ErrInvalidDBFile
	}

	if info.Size() < wantedSize {
		if err := f.Truncate(wantedSize); err != nil {
			_ = f.Close()
			return nil, err
		}
	}

	if info.Size() >= CelulaSize {
		cursor, nextPhase := db.recoverState()
		db.cursor = cursor
		db.phase = nextPhase
	} else {
		// Archivo nuevo
		db.cursor = 0
		db.phase = true
	}

	return db, nil
}

// OpenExistingOuroborosDB abre una base existente y deriva maxRecords desde el tamaño del archivo.
// Falla si el archivo no existe o si su tamaño no es múltiplo del tamaño de celda.
func OpenExistingOuroborosDB(path string) (*OuroborosDB, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	size := info.Size()
	if size < CelulaSize || size%CelulaSize != 0 {
		_ = f.Close()
		return nil, ErrInvalidDBFile
	}

	maxRecords64 := size / CelulaSize
	if maxRecords64 <= 0 || maxRecords64 > int64(^uint32(0)) {
		_ = f.Close()
		return nil, ErrInvalidDBFile
	}

	db := &OuroborosDB{
		file:       f,
		maxRecords: uint32(maxRecords64),
	}

	cursor, nextPhase := db.recoverState()
	db.cursor = cursor
	db.phase = nextPhase

	return db, nil
}

// ---------- RECUPERACIÓN ----------
func (db *OuroborosDB) recoverState() (uint32, bool) {
	c0 := db.readRaw(0)
	clast := db.readRaw(db.maxRecords - 1)

	firstPhase := getGhost(c0.Genoma)
	lastPhase := getGhost(clast.Genoma)

	if firstPhase == lastPhase {
		return 0, !firstPhase
	}

	var low uint32 = 0
	var high uint32 = db.maxRecords - 1

	for low < high {
		mid := (low + high) / 2
		cmid := db.readRaw(mid)

		if getGhost(cmid.Genoma) == firstPhase {
			low = mid + 1
		} else {
			high = mid
		}
	}
	return low, firstPhase
}

// readRaw interno, sin mutex, usado de manera segura internamente
func (db *OuroborosDB) readRaw(index uint32) Celula {
	buf := make([]byte, CelulaSize)
	db.file.ReadAt(buf, int64(index)*CelulaSize)
	return fromBytes(buf)
}

// ---------- APPEND (Writer) ----------
func (db *OuroborosDB) Append(c Celula) (uint32, error) {
	db.mu.Lock() // SWMR: Exclusividad para escribir
	defer db.mu.Unlock()

	genomaAjustado := setGhost(c.Genoma, db.phase)

	// Clonamos y ajustamos
	nuevaCel := c
	nuevaCel.Genoma = genomaAjustado
	buffer := nuevaCel.toBytes()

	offset := int64(db.cursor) * CelulaSize
	if _, err := db.file.WriteAt(buffer, offset); err != nil {
		return 0, err
	}

	saved := db.cursor
	db.cursor++
	if db.cursor >= db.maxRecords {
		db.cursor = 0
		db.phase = !db.phase
	}

	return saved, nil
}

// ---------- READ (Reader) ----------
func (db *OuroborosDB) Read(index uint32) (Celula, error) {
	if index >= db.maxRecords {
		return Celula{}, ErrOutOfBounds
	}

	db.mu.RLock() // SWMR: Múltiples lecturas simultáneas permitidas
	defer db.mu.RUnlock()

	return db.readRaw(index), nil
}

// ---------- READ_AUTH (Reader) ----------
func (db *OuroborosDB) ReadAuth(index uint32, secret []byte) (Celula, error) {
	c, err := db.Read(index)
	if err != nil {
		return Celula{}, err
	}

	data := append(c.Salt[:], secret...)
	hashEsperado := blake3.Sum256(data)

	if !bytes.Equal(hashEsperado[:], c.Hash[:]) {
		return Celula{}, ErrUnauthorized
	}

	return c, nil
}

// ---------- UPDATE (Writer) ----------
func (db *OuroborosDB) Update(index uint32, nuevoGenoma, x, y, z uint32) error {
	db.mu.Lock() // SWMR: Exclusividad para escribir
	defer db.mu.Unlock()

	if index >= db.maxRecords {
		return ErrOutOfBounds
	}

	cActual := db.readRaw(index)
	faseOriginal := getGhost(cActual.Genoma)
	genomaFinal := setGhost(nuevoGenoma, faseOriginal)

	nuevaCel := Celula{
		Hash:   cActual.Hash,
		Salt:   cActual.Salt,
		Genoma: genomaFinal,
		X:      x,
		Y:      y,
		Z:      z,
	}

	buffer := nuevaCel.toBytes()
	_, err := db.file.WriteAt(buffer, int64(index)*CelulaSize)
	return err
}

// ---------- UPDATE_AUTH (Writer) ----------
func (db *OuroborosDB) UpdateAuth(index uint32, secret []byte, nuevoGenoma, x, y, z uint32) error {
	// Valida autorización como lector primero (ReadAuth adquiere RLock)
	cActual, err := db.ReadAuth(index, secret)
	if err != nil {
		return err
	}

	// Escala a escritor
	db.mu.Lock()
	defer db.mu.Unlock()

	faseOriginal := getGhost(cActual.Genoma)
	genomaFinal := setGhost(nuevoGenoma, faseOriginal)

	nuevaCel := Celula{
		Hash:   cActual.Hash,
		Salt:   cActual.Salt,
		Genoma: genomaFinal,
		X:      x,
		Y:      y,
		Z:      z,
	}

	buffer := nuevaCel.toBytes()
	_, err = db.file.WriteAt(buffer, int64(index)*CelulaSize)
	return err
}
