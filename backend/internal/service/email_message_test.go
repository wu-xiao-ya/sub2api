package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildSMTPMessage_UsesStandardHeadersAndEnvelopeAddresses(t *testing.T) {
	message, err := buildSMTPMessage(
		&SMTPConfig{
			Host:     "smtp.example.com",
			From:     "Starlight <sender@example.com>",
			FromName: "Star Light",
		},
		"Reader <reader@example.com>",
		"Chinese subject",
		"<p>status=value</p>",
	)
	require.NoError(t, err)
	require.Equal(t, "sender@example.com", message.envelopeFrom)
	require.Equal(t, "reader@example.com", message.envelopeTo)

	data := string(message.data)
	require.Contains(t, data, "Date: ")
	require.Contains(t, data, "Message-ID: <")
	require.Contains(t, data, "MIME-Version: 1.0")
	require.Contains(t, data, "Content-Type: text/html; charset=UTF-8")
	require.Contains(t, data, "Content-Transfer-Encoding: quoted-printable")
	require.True(t, strings.Contains(data, "status=3Dvalue"))
}

func TestBuildSMTPMessage_RejectsRecipientHeaderInjection(t *testing.T) {
	_, err := buildSMTPMessage(
		&SMTPConfig{Host: "smtp.example.com", From: "sender@example.com"},
		"reader@example.com\r\nBcc: attacker@example.com",
		"Subject",
		"Body",
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "line break")
}
