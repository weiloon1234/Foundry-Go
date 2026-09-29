package oauth

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/secret"
)

type githubUser struct {
	ID        json.Number `json:"id"`
	Login     string      `json:"login"`
	Name      string      `json:"name"`
	Email     string      `json:"email"`
	AvatarURL string      `json:"avatar_url"`
}

type githubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

func (c *Client) githubRequest(address string, access secret.String) httpclient.Request {
	return c.http.Get(address).Bearer(access).Header("Accept", "application/vnd.github+json").
		Header("X-GitHub-Api-Version", "2022-11-28").Header("User-Agent", "Foundry-Go")
}

// githubProfile reads the account from the user API. The public profile email
// is never treated as verified; the verified primary address comes from the
// emails API when the user:email scope was granted.
func (c *Client) githubProfile(ctx context.Context, access secret.String) (Profile, error) {
	response, err := c.http.Do(ctx, c.githubRequest(c.provider.UserURL, access))
	if err != nil {
		return Profile{}, err
	}
	if err := response.EnsureSuccess(); err != nil {
		return Profile{}, err
	}
	var user githubUser
	if err := decodeDocument(response.Bytes(), &user); err != nil {
		return Profile{}, err
	}
	id, err := strconv.ParseInt(user.ID.String(), 10, 64)
	if err != nil || id <= 0 || user.Login == "" {
		return Profile{}, fault.New(fault.Invalid, "GitHub user response has no account ID")
	}
	profile := Profile{Subject: strconv.FormatInt(id, 10), Username: user.Login, Name: user.Name, Email: user.Email, AvatarURL: user.AvatarURL}
	if profile.Name == "" {
		profile.Name = user.Login
	}
	if c.provider.EmailsURL == "" {
		return profile, nil
	}
	emails, err := c.http.Do(ctx, c.githubRequest(c.provider.EmailsURL, access))
	if err != nil {
		return Profile{}, err
	}
	switch status := emails.Status(); {
	case status == 403 || status == 404:
		return profile, nil // user:email was not granted.
	case status < 200 || status > 299:
		return Profile{}, emails.EnsureSuccess()
	}
	var addresses []githubEmail
	if err := decodeDocument(emails.Bytes(), &addresses); err != nil {
		return Profile{}, err
	}
	for _, address := range addresses {
		if address.Primary && address.Verified && address.Email != "" {
			profile.Email, profile.EmailVerified = address.Email, true
			break
		}
	}
	return profile, nil
}
