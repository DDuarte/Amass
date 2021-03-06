// Copyright 2017 Jeff Foley. All rights reserved.
// Use of this source code is governed by Apache 2 LICENSE that can be found in the LICENSE file.

package amass

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/OWASP/Amass/amass/utils"
	"github.com/dghubble/go-twitter/twitter"
	"golang.org/x/oauth2"
)

// Twitter is the Service that handles access to the Twitter data source.
type Twitter struct {
	BaseService

	API        *APIKey
	SourceType string
	RateLimit  time.Duration
	client     *twitter.Client
}

// NewTwitter returns he object initialized, but not yet started.
func NewTwitter(e *Enumeration) *Twitter {
	t := &Twitter{
		SourceType: API,
		RateLimit:  3 * time.Second,
	}

	t.BaseService = *NewBaseService(e, "Twitter", t)
	return t
}

// OnStart implements the Service interface
func (t *Twitter) OnStart() error {
	t.BaseService.OnStart()

	t.API = t.Enum().Config.GetAPIKey(t.String())
	if t.API == nil || t.API.Key == "" || t.API.Secret == "" {
		t.Enum().Log.Printf("%s: API key data was not provided", t.String())
	}
	if t.API != nil && t.API.Key != "" && t.API.Secret != "" {
		if bearer, err := t.getBearerToken(); err == nil {
			config := &oauth2.Config{}
			token := &oauth2.Token{AccessToken: bearer}
			// OAuth2 http.Client will automatically authorize Requests
			httpClient := config.Client(oauth2.NoContext, token)
			// Twitter client
			t.client = twitter.NewClient(httpClient)
		}
	}
	go t.startRootDomains()
	go t.processRequests()
	return nil
}

func (t *Twitter) processRequests() {
	for {
		select {
		case <-t.PauseChan():
			<-t.ResumeChan()
		case <-t.Quit():
			return
		case <-t.RequestChan():
			// This data source just throws away the checked DNS names
			t.SetActive()
		}
	}
}

func (t *Twitter) startRootDomains() {
	// Look at each domain provided by the config
	for _, domain := range t.Enum().Config.Domains() {
		t.executeQuery(domain)
		// Honor the rate limit
		time.Sleep(t.RateLimit)
	}
}

func (t *Twitter) executeQuery(domain string) {
	if t.client == nil {
		return
	}

	searchParams := &twitter.SearchTweetParams{
		Query: domain,
		Count: 100,
	}
	search, _, err := t.client.Search.Tweets(searchParams)
	if err != nil {
		t.Enum().Log.Printf("%s: %v", t.String(), err)
		return
	}

	t.SetActive()
	re := t.Enum().Config.DomainRegex(domain)
	for _, tweet := range search.Statuses {
		for _, name := range re.FindAllString(tweet.Text, -1) {
			t.Enum().NewNameEvent(&Request{
				Name:   name,
				Domain: domain,
				Tag:    t.SourceType,
				Source: t.String(),
			})
		}
	}
}

func (t *Twitter) getBearerToken() (string, error) {
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded;charset=UTF-8"}
	page, err := utils.RequestWebPage(
		"https://api.twitter.com/oauth2/token",
		strings.NewReader("grant_type=client_credentials"),
		headers, t.API.Key, t.API.Secret)
	if err != nil {
		return "", fmt.Errorf("token request failed: %+v", err)
	}

	var v struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal([]byte(page), &v); err != nil {
		return "", fmt.Errorf("error parsing json in token response: %+v", err)
	}
	if v.AccessToken == "" {
		return "", fmt.Errorf("token response does not have access_token")
	}
	return v.AccessToken, nil
}
