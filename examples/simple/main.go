package main

import (
	"fmt"
	"log"
	//"os"
	"crypto/rand"

	ouroboros "github.com/DiegoSandival/ouroboros-go"
	"lukechampine.com/blake3"
)

func main() {
	secret := []byte("demo-secret")
	var salt [16]byte

	// Read llena el slice con bytes aleatorios seguros
	_, err := rand.Read(salt[:])
	if err != nil {
		// Este error es extremadamente raro, pero debe manejarse
		log.Fatal(err)
	}

	data := append(salt[:], secret...)
	hash := blake3.Sum256(data)

	cell := ouroboros.Celula{
		Hash:   hash,
		Salt:   salt,
		Genoma: ouroboros.LeerSelf | ouroboros.EscribirSelf,
		X:      10,
		Y:      234,
		Z:      4234,
	}

	path := "./ouroboros-demo1.db"
	//defer os.Remove(path)

	db, err := ouroboros.OpenOuroborosDB(path, 16)
	if err != nil {
		log.Fatal(err)
	}

	index, err := db.Append(cell)
	if err != nil {
		log.Fatal(err)
	}

	stored, err := db.Read(index)
	if err != nil {
		log.Fatal(err)
	}

	authorized, err := db.ReadAuth(index, secret)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("append index=%d\n", index)
	fmt.Printf("read coords=(%d,%d,%d)\n", stored.X, stored.Y, stored.Z)
	fmt.Printf("read_auth genome=%d\n", authorized.Genoma)
	fmt.Println("example completed")
}