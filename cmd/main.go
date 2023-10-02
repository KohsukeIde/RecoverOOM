package main

import (
	"context"
	"fmt"
	"os"

	RecoverOOM "https://github.com/KohsukeIde/RecoverOOM"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Println("Not enough argument provided")
		return
	}

	projectID := os.Args[1]
	functionRegion := os.Args[2]
	clusterName := os.Args[3]

	// projectID := os.Getenv("PROJECT_ID")
	// webhookUrl := os.Getenv("SLACK_WEBHOOK_URL")
	// channelFatal := os.Getenv("CHANNEL_FATAL")
	// channelDebug := os.Getenv("CHANNEL_DEBUG")
	webhookUrl := ""
	channelFatal := ""
	channelDebug := ""

	// if projectID == "" || webhookUrl == "" || channelFatal == "" || channelDebug == "" {
	// 	fmt.Fprintf(os.Stderr, "invalid env vars: PROJECT_ID=%s, SLACK_WEBHOOK_URL=%s, CHANNEL_FATAL=%s, CHANNEL_DEBUG=%s\n", projectID, webhookUrl, channelFatal, channelDebug)
	// 	os.Exit(1)
	// }

	option := RecoverOOM.Option{
		ProjectID: projectID,

		MaxDisplaySameErrors: 5,

		Username:  "Mega Charizard X",
		IconEmoji: ":mega_charidard_x:",

		WebhookUrl:   webhookUrl,
		ChannelFatal: channelFatal,
		ChannelDebug: channelDebug,
	}

	if err := RecoverOOM.Run(context.Background(), option, projectID, functionRegion, clusterName); err != nil {
		fmt.Fprintf(os.Stderr, "RecoverOOM.Run: %+v\n", err)
		os.Exit(1)
	}
}
