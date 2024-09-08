package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/dustin/go-humanize"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/fatih/color"

	"github.com/urfave/cli/v2"
)

func doShowProfile(cCtx *cli.Context) error {
	if cCtx.Args().Present() {
		return cli.ShowSubcommandHelp(cCtx)
	}

	xrpcc, err := makeXRPCC(cCtx)
	if err != nil {
		return fmt.Errorf("cannot create client: %w", err)
	}

	arg := cCtx.String("handle")
	if arg == "" {
		arg = xrpcc.Auth.Handle
	}

	profile, err := bsky.ActorGetProfile(context.TODO(), xrpcc, arg)
	if err != nil {
		return fmt.Errorf("cannot get profile: %w", err)
	}

	if cCtx.Bool("json") {
		json.NewEncoder(os.Stdout).Encode(profile)
		return nil
	}

	fmt.Printf("Did: %s\n", profile.Did)
	fmt.Printf("Handle: %s\n", profile.Handle)
	fmt.Printf("DisplayName: %s\n", stringp(profile.DisplayName))
	fmt.Printf("Description: %s\n", stringp(profile.Description))
	fmt.Printf("Follows: %d\n", int64p(profile.FollowsCount))
	fmt.Printf("Followers: %d\n", int64p(profile.FollowersCount))
	fmt.Printf("Avatar: %s\n", stringp(profile.Avatar))
	fmt.Printf("Banner: %s\n", stringp(profile.Banner))
	return nil
}

func doUpdateProfile(cCtx *cli.Context) error {
	// read arguments
	var name *string
	if cCtx.Args().Len() >= 1 {
		v := cCtx.Args().Get(0)
		name = &v
	}
	var desc *string
	if cCtx.Args().Len() >= 2 {
		v := cCtx.Args().Get(1)
		desc = &v
	}
	// read options
	var avatarFn *string
	if s := cCtx.String("avatar"); s != "" {
		avatarFn = &s
	}
	var bannerFn *string
	if s := cCtx.String("banner"); s != "" {
		bannerFn = &s
	}

	if name == nil && desc == nil && avatarFn == nil && bannerFn == nil {
		return cli.ShowSubcommandHelp(cCtx)
	}

	xrpcc, err := makeXRPCC(cCtx)
	if err != nil {
		return fmt.Errorf("cannot create client: %w", err)
	}

	currentProfile, err := bsky.ActorGetProfile(context.TODO(), xrpcc, xrpcc.Auth.Handle)
	if err != nil {
		return fmt.Errorf("cannot get profile: %w", err)
	}
	if name == nil {
		name = currentProfile.DisplayName
	}
	if desc == nil {
		desc = currentProfile.Description
	}

	var avatar *lexutil.LexBlob
	if currentProfile.Avatar != nil {
		currentAvatarCid, currentAvatarType, err := ParseCid(currentProfile.Avatar)
		if err != nil {
			return fmt.Errorf("cannot parse avatar cid: %w", err)
		}
		avatar = &lexutil.LexBlob{
			Ref:      lexutil.LexLink(currentAvatarCid),
			MimeType: currentAvatarType,
		}
	}
	if avatarFn != nil {
		b, err := os.ReadFile(*avatarFn)
		if err != nil {
			return fmt.Errorf("cannot read image file: %w", err)
		}

		resp, err := comatproto.RepoUploadBlob(context.TODO(), xrpcc, bytes.NewReader(b))
		if err != nil {
			return fmt.Errorf("cannot upload image file: %w", err)
		}
		avatar = &lexutil.LexBlob{
			Ref:      resp.Blob.Ref,
			MimeType: http.DetectContentType(b),
			Size:     resp.Blob.Size,
		}
	}

	var banner *lexutil.LexBlob
	if currentProfile.Banner != nil {
		currentBannerCid, currentBannerType, err := ParseCid(currentProfile.Banner)
		if err != nil {
			return fmt.Errorf("cannot parse banner cid: %w", err)
		}
		banner = &lexutil.LexBlob{
			Ref:      lexutil.LexLink(currentBannerCid),
			MimeType: currentBannerType,
		}
	}

	if bannerFn != nil {
		b, err := os.ReadFile(*bannerFn)
		if err != nil {
			return fmt.Errorf("cannot read image file: %w", err)
		}
		resp, err := comatproto.RepoUploadBlob(context.TODO(), xrpcc, bytes.NewReader(b))
		if err != nil {
			return fmt.Errorf("cannot upload image file: %w", err)
		}
		banner = &lexutil.LexBlob{
			Ref:      resp.Blob.Ref,
			MimeType: http.DetectContentType(b),
			Size:     resp.Blob.Size,
		}
	}
	currentRecord, err := comatproto.RepoGetRecord(context.TODO(), xrpcc, "", "app.bsky.actor.profile", xrpcc.Auth.Did, "self")
	if err != nil {
		return fmt.Errorf("cannot get profile: %w", err)
	}

	updatedRecord := &lexutil.LexiconTypeDecoder{Val: &bsky.ActorProfile{
		Description: desc,
		DisplayName: name,
		Avatar:      avatar,
		Banner:      banner,
	}}

	_, err = comatproto.RepoPutRecord(context.TODO(), xrpcc, &comatproto.RepoPutRecord_Input{
		Repo:       xrpcc.Auth.Did,
		Collection: "app.bsky.actor.profile",
		Rkey:       "self",
		Record:     updatedRecord,
		SwapRecord: currentRecord.Cid,
	})

	if err != nil {
		return fmt.Errorf("cannot update profile: %w", err)
	}
	return nil
}

func doFollow(cCtx *cli.Context) error {
	if !cCtx.Args().Present() {
		return cli.ShowSubcommandHelp(cCtx)
	}

	xrpcc, err := makeXRPCC(cCtx)
	if err != nil {
		return fmt.Errorf("cannot create client: %w", err)
	}

	for _, arg := range cCtx.Args().Slice() {
		profile, err := bsky.ActorGetProfile(context.TODO(), xrpcc, arg)
		if err != nil {
			return fmt.Errorf("cannot get profile: %w", err)
		}

		follow := bsky.GraphFollow{
			LexiconTypeID: "app.bsky.graph.follow",
			CreatedAt:     time.Now().Local().Format(time.RFC3339),
			Subject:       profile.Did,
		}

		resp, err := comatproto.RepoCreateRecord(context.TODO(), xrpcc, &comatproto.RepoCreateRecord_Input{
			Collection: "app.bsky.graph.follow",
			Repo:       xrpcc.Auth.Did,
			Record: &lexutil.LexiconTypeDecoder{
				Val: &follow,
			},
		})
		if err != nil {
			return err
		}
		fmt.Println(resp.Uri)
	}
	return nil
}

func doUnfollow(cCtx *cli.Context) error {
	if !cCtx.Args().Present() {
		return cli.ShowSubcommandHelp(cCtx)
	}

	xrpcc, err := makeXRPCC(cCtx)
	if err != nil {
		return fmt.Errorf("cannot create client: %w", err)
	}

	for _, arg := range cCtx.Args().Slice() {
		profile, err := bsky.ActorGetProfile(context.TODO(), xrpcc, arg)
		if err != nil {
			return fmt.Errorf("cannot get profile: %w", err)
		}

		if profile.Viewer.Following == nil {
			continue
		}

		parts := strings.Split(*profile.Viewer.Following, "/")
		if len(parts) < 3 {
			return fmt.Errorf("invalid post uri: %q", arg)
		}
		rkey := parts[len(parts)-1]
		schema := parts[len(parts)-2]
		fmt.Println(stringp(profile.Viewer.Following))
		err = comatproto.RepoDeleteRecord(context.TODO(), xrpcc, &comatproto.RepoDeleteRecord_Input{
			Repo:       xrpcc.Auth.Did,
			Collection: schema,
			Rkey:       rkey,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func doFollows(cCtx *cli.Context) error {
	if cCtx.Args().Present() {
		return cli.ShowSubcommandHelp(cCtx)
	}

	xrpcc, err := makeXRPCC(cCtx)
	if err != nil {
		return fmt.Errorf("cannot create client: %w", err)
	}

	arg := cCtx.String("handle")
	if arg == "" {
		arg = xrpcc.Auth.Handle
	}

	var cursor string
	for {
		follows, err := bsky.GraphGetFollows(context.TODO(), xrpcc, arg, cursor, 100)
		if err != nil {
			return fmt.Errorf("getting record: %w", err)
		}

		if cCtx.Bool("json") {
			for _, f := range follows.Follows {
				json.NewEncoder(os.Stdout).Encode(f)
			}
		} else {
			for _, f := range follows.Follows {
				color.Set(color.FgHiRed)
				fmt.Print(f.Handle)
				color.Set(color.Reset)
				fmt.Printf(" [%s] ", stringp(f.DisplayName))
				color.Set(color.FgBlue)
				fmt.Println(f.Did)
				color.Set(color.Reset)
			}
		}
		if follows.Cursor == nil {
			break
		}
		cursor = *follows.Cursor
	}
	return nil
}

func doInactiveFollows(cCtx *cli.Context) error {
	if cCtx.Args().Present() {
		return cli.ShowSubcommandHelp(cCtx)
	}

	xrpcc, err := makeXRPCC(cCtx)
	if err != nil {
		return fmt.Errorf("cannot create client: %w", err)
	}

	arg := cCtx.String("handle")
	if arg == "" {
		arg = xrpcc.Auth.Handle
	}

	var cursor string
	for {
		follows, err := bsky.GraphGetFollows(context.TODO(), xrpcc, arg, cursor, 100)
		if err != nil {
			return fmt.Errorf("getting record: %w", err)
		}

		strict := cCtx.Bool("strict")

		if cCtx.Bool("json") {
			for _, f := range follows.Follows {
				json.NewEncoder(os.Stdout).Encode(f)
			}
		} else {
			for _, f := range follows.Follows {
				latestPost, err := getLatestPost(xrpcc, f.Handle)
				if err != nil {
					return fmt.Errorf("getting latest post: %w", err)
					break
				}
				followsMe := f.Viewer.FollowedBy != nil
				if strict && followsMe {
					continue
				}
				if latestPost == nil || (latestPost != nil && latestPost.Post == nil) {
					outputAccount(f, color.FgHiRed, "never", followsMe)
				} else {
					var prettyTime string
					cor := color.FgRed

					dtime, err := time.Parse(time.RFC3339, latestPost.Post.IndexedAt)
					if err != nil {
						// fallback to index time
						prettyTime = latestPost.Post.IndexedAt
					} else {
						cor = colorFor(dtime)
						prettyTime = humanize.Time(dtime)
					}

					inactiveColors := []color.Attribute{color.FgRed, color.FgYellow}
					if strict && !slices.Contains(inactiveColors, cor) {
						continue
					}

					outputAccount(f, cor, prettyTime, followsMe)
				}
			}
		}
		if follows.Cursor == nil {
			break
		}
		cursor = *follows.Cursor
	}
	return nil
}

func outputAccount(f *bsky.ActorDefs_ProfileView, statusColor color.Attribute, prettyTime string, followsMe bool) {
	color.Set(color.FgHiRed)
	fmt.Print(f.Handle)
	color.Set(color.Reset)
	fmt.Printf(" [%s] ", stringp(f.DisplayName))
	color.Set(color.FgBlue)
	fmt.Print(f.Did)
	color.Set(color.Reset)
	if followsMe {
		color.Set(color.FgGreen)
		fmt.Printf(" [💚] ")
	} else {
		color.Set(color.FgRed)
		fmt.Printf(" [❌] ")
	}
	color.Set(color.Reset)
	color.Set(statusColor)
	fmt.Printf(" [%s]\n", prettyTime)
	color.Set(color.Reset)
}

func colorFor(date time.Time) color.Attribute {
	duration := time.Now().Sub(date)

	// Calculate weeks, months, and years ago
	weeksAgo := 7 * 24 * time.Hour
	monthAgo := 30 * 24 * time.Hour
	yearAgo := 365 * 24 * time.Hour

	var timeColor color.Attribute
	switch {
	case duration <= weeksAgo:
		timeColor = color.FgGreen // Within the last week
	case duration <= monthAgo:
		timeColor = color.FgCyan // Within the last month
	case duration <= yearAgo:
		timeColor = color.FgYellow // Within the last year
	default:
		timeColor = color.FgRed // More than a year ago
	}

	return timeColor
}

func getLatestPost(xrpcc *xrpc.Client, handle string) (*bsky.FeedDefs_FeedViewPost, error) {
	resp, err := bsky.FeedGetAuthorFeed(context.TODO(), xrpcc, handle, "", "", 1)
	if err != nil {
		return nil, fmt.Errorf("cannot get author feed: %w", err)
	}
	if len(resp.Feed) == 0 {
		return nil, nil
	}
	return resp.Feed[0], nil
}

func doFollowers(cCtx *cli.Context) error {
	if cCtx.Args().Present() {
		return cli.ShowSubcommandHelp(cCtx)
	}

	xrpcc, err := makeXRPCC(cCtx)
	if err != nil {
		return fmt.Errorf("cannot create client: %w", err)
	}

	arg := cCtx.String("handle")
	if arg == "" {
		arg = xrpcc.Auth.Handle
	}

	var cursor string
	for {
		followers, err := bsky.GraphGetFollowers(context.TODO(), xrpcc, arg, cursor, 100)
		if err != nil {
			return fmt.Errorf("getting record: %w", err)
		}

		if cCtx.Bool("json") {
			for _, f := range followers.Followers {
				json.NewEncoder(os.Stdout).Encode(f)
			}
		} else {
			for _, f := range followers.Followers {
				color.Set(color.FgHiRed)
				fmt.Print(f.Handle)
				color.Set(color.Reset)
				fmt.Printf(" [%s] ", stringp(f.DisplayName))
				color.Set(color.FgBlue)
				fmt.Println(f.Did)
				color.Set(color.Reset)
			}
		}
		if followers.Cursor == nil {
			break
		}
		cursor = *followers.Cursor
	}
	return nil
}

func doBlock(cCtx *cli.Context) error {
	if !cCtx.Args().Present() {
		return cli.ShowSubcommandHelp(cCtx)
	}

	xrpcc, err := makeXRPCC(cCtx)
	if err != nil {
		return fmt.Errorf("cannot create client: %w", err)
	}

	for _, arg := range cCtx.Args().Slice() {
		profile, err := bsky.ActorGetProfile(context.TODO(), xrpcc, arg)
		if err != nil {
			return fmt.Errorf("cannot get profile: %w", err)
		}

		block := bsky.GraphBlock{
			LexiconTypeID: "app.bsky.graph.block",
			CreatedAt:     time.Now().Local().Format(time.RFC3339),
			Subject:       profile.Did,
		}

		resp, err := comatproto.RepoCreateRecord(context.TODO(), xrpcc, &comatproto.RepoCreateRecord_Input{
			Collection: "app.bsky.graph.block",
			Repo:       xrpcc.Auth.Did,
			Record: &lexutil.LexiconTypeDecoder{
				Val: &block,
			},
		})
		if err != nil {
			return err
		}
		fmt.Println(resp.Uri)
	}
	return nil
}

func doUnblock(cCtx *cli.Context) error {
	if !cCtx.Args().Present() {
		return cli.ShowSubcommandHelp(cCtx)
	}

	xrpcc, err := makeXRPCC(cCtx)
	if err != nil {
		return fmt.Errorf("cannot create client: %w", err)
	}

	for _, arg := range cCtx.Args().Slice() {
		profile, err := bsky.ActorGetProfile(context.TODO(), xrpcc, arg)
		if err != nil {
			return fmt.Errorf("cannot get profile: %w", err)
		}

		if profile.Viewer.Blocking == nil {
			continue
		}

		parts := strings.Split(*profile.Viewer.Blocking, "/")
		if len(parts) < 3 {
			return fmt.Errorf("invalid post uri: %q", arg)
		}
		rkey := parts[len(parts)-1]
		schema := parts[len(parts)-2]
		fmt.Println(stringp(profile.Viewer.Blocking))
		err = comatproto.RepoDeleteRecord(context.TODO(), xrpcc, &comatproto.RepoDeleteRecord_Input{
			Repo:       xrpcc.Auth.Did,
			Collection: schema,
			Rkey:       rkey,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func doBlocks(cCtx *cli.Context) error {
	if cCtx.Args().Present() {
		return cli.ShowSubcommandHelp(cCtx)
	}

	xrpcc, err := makeXRPCC(cCtx)
	if err != nil {
		return fmt.Errorf("cannot create client: %w", err)
	}

	var cursor string
	for {
		blocks, err := bsky.GraphGetBlocks(context.TODO(), xrpcc, cursor, 100)
		if err != nil {
			return fmt.Errorf("getting record: %w", err)
		}

		if cCtx.Bool("json") {
			for _, f := range blocks.Blocks {
				json.NewEncoder(os.Stdout).Encode(f)
			}
		} else {
			for _, f := range blocks.Blocks {
				color.Set(color.FgHiRed)
				fmt.Print(f.Handle)
				color.Set(color.Reset)
				fmt.Printf(" [%s] ", stringp(f.DisplayName))
				color.Set(color.FgBlue)
				fmt.Println(f.Did)
				color.Set(color.Reset)
			}
		}
		if blocks.Cursor == nil {
			break
		}
		cursor = *blocks.Cursor
	}
	return nil
}

func doLogin(cCtx *cli.Context) error {
	fp, _ := cCtx.App.Metadata["path"].(string)
	var cfg config
	cfg.Host = cCtx.String("host")
	cfg.Bgs = cCtx.String("bgs")
	cfg.Handle = cCtx.Args().Get(0)
	cfg.Password = cCtx.Args().Get(1)
	if cfg.Handle == "" || cfg.Password == "" {
		cli.ShowSubcommandHelpAndExit(cCtx, 1)
	}
	b, err := json.MarshalIndent(&cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot make config file: %w", err)
	}
	err = os.WriteFile(fp, b, 0644)
	if err != nil {
		return fmt.Errorf("cannot write config file: %w", err)
	}
	return nil
}

func doNotification(cCtx *cli.Context) error {
	if cCtx.Args().Present() {
		return cli.ShowSubcommandHelp(cCtx)
	}

	xrpcc, err := makeXRPCC(cCtx)
	if err != nil {
		return fmt.Errorf("cannot create client: %w", err)
	}

	notifs, err := bsky.NotificationListNotifications(context.TODO(), xrpcc, "", 50, "")
	if err != nil {
		return err
	}

	if cCtx.Bool("json") {
		for _, n := range notifs.Notifications {
			json.NewEncoder(os.Stdout).Encode(n)
		}
		return nil
	}

	for _, n := range notifs.Notifications {
		if !cCtx.Bool("a") && n.IsRead {
			continue
		}
		color.Set(color.FgHiRed)
		fmt.Print(n.Author.Handle)
		color.Set(color.Reset)
		fmt.Printf(" [%s] ", stringp(n.Author.DisplayName))
		color.Set(color.FgBlue)
		fmt.Println(n.Author.Did)
		color.Set(color.Reset)

		switch v := n.Record.Val.(type) {
		case *bsky.FeedPost:
			fmt.Println(" " + n.Reason + " to " + n.Uri)
		case *bsky.FeedRepost:
			fmt.Printf(" reposted %s\n", v.Subject.Uri)
		case *bsky.FeedLike:
			fmt.Printf(" liked %s\n", v.Subject.Uri)
		case *bsky.GraphFollow:
			fmt.Println(" followed you")
		}

		bsky.NotificationUpdateSeen(context.TODO(), xrpcc, &bsky.NotificationUpdateSeen_Input{
			SeenAt: time.Now().Local().Format(time.RFC3339),
		})
	}

	return nil
}

func doShowSession(cCtx *cli.Context) error {
	xrpcc, err := makeXRPCC(cCtx)
	if err != nil {
		return fmt.Errorf("cannot create client: %w", err)
	}

	session, err := comatproto.ServerGetSession(context.TODO(), xrpcc)
	if err != nil {
		return err
	}

	if cCtx.Bool("json") {
		json.NewEncoder(os.Stdout).Encode(session)
		return nil
	}

	fmt.Printf("Did: %s\n", session.Did)
	fmt.Printf("Email: %s\n", stringp(session.Email))
	fmt.Printf("Handle: %s\n", session.Handle)
	return nil
}

func doInviteCodes(cCtx *cli.Context) error {
	xrpcc, err := makeXRPCC(cCtx)
	if err != nil {
		return fmt.Errorf("cannot create client: %w", err)
	}

	includeUsed := cCtx.Bool("used")

	codes, err := comatproto.ServerGetAccountInviteCodes(context.TODO(), xrpcc, false, includeUsed)
	if err != nil {
		return err
	}

	if cCtx.Bool("json") {
		for _, c := range codes.Codes {
			json.NewEncoder(os.Stdout).Encode(c)
		}
		return nil
	}

	for _, c := range codes.Codes {
		if int64(len(c.Uses)) >= c.Available { // used
			color.Set(color.FgHiMagenta)
			fmt.Printf("%s (used)\n", c.Code)
			color.Set(color.Reset)
		} else {
			fmt.Println(c.Code)
		}
	}

	return nil
}
