package main

import (
	"github.com/jonas747/discordgo"
	"log"
)

func (fs *DiscordFS) OnMessageCreate(s *discordgo.Session, r *discordgo.MessageCreate) {
	if fs.Guild == r.ChannelID {
		fs.InvalidateCache()
	}
}
func (fs *DiscordFS) OnMessageRemove(s *discordgo.Session, r *discordgo.MessageDelete) {
	if fs.Guild == r.ChannelID {
		fs.InvalidateCache()
	}
}
func (fs *DiscordFS) OnMessageEdit(s *discordgo.Session, e *discordgo.MessageUpdate) {
	if fs.Guild == e.ChannelID {
		fs.InvalidateCache()
	}
}

func (fs *DiscordFS) OnChannelEdit(s *discordgo.Session, c *discordgo.ChannelUpdate) {
	if fs.Guild == c.Channel.ID {
		fs.InvalidateCache()
	}
}

func (fs *DiscordFS) InvalidateCache() {
	log.Println("!PURGING CACHE!")
	fs.cache.Purge()
}
