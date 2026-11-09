package main

import (
	"github.com/df-mc/dragonfly/server/player/chat"
	"golang.org/x/text/language"
)

// shutdownMessage is the reason every online player gets when the server stops. It is plain text,
// not Dragonfly's default Bedrock translation key (%disconnect.disconnected): proxies such as
// Velocity show the reason in chat as it is, so a key would be shown raw.
const shutdownMessage = "The server closed. Join back in a few seconds."

// plainText is a chat.TranslationString of fixed text.
type plainText string

func (t plainText) Resolve(language.Tag) string { return string(t) }

// shutdownTranslation is shutdownMessage as the chat.Translation server.Config wants.
var shutdownTranslation = chat.Translate(plainText(shutdownMessage), 0, shutdownMessage)
