package main

import (
	"flag"
	"github.com/jonas747/discordgo"
	"log"
)

func main() {
	flag.Parse()
	if len(flag.Args()) < 3 {
		log.Fatal("Usage:\n  discord-fs TOKEN GUILDID MOUNTPOINT")
	}
	log.SetFlags(log.Lmicroseconds)

	log.Println("Starting discord-fs")
	session, err := discordgo.New(flag.Arg(0))
	if err != nil {
		panic(err)
	}
	NewFS(session, flag.Arg(1))

	err = session.Open()
	if err != nil {
		panic(err)
	}

	select {}
}
