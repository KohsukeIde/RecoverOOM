package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"https://github.com/KohsukeIde/RecoverOOM"

	"cloud.google.com/go/pubsub"
)

type PubSubMessage struct {
	JsonPayload jsonPayload
	Event       event
	Resource    resource
}
type jsonPayload struct {
	Reason string
}

type event struct {
	Resource resource
}

type resource struct {
	Labels resourcelabels

	Type string
}

type resourcelabels struct {
	Cluster_Name string
	Location     string
	Project_Id   string
}

func main() {
	projectID := os.Getenv("PROJECT_ID")
	if projectID == "" {
		panic("projectId empty")
	}
	// subID := os.Getenv("SUB-ID")

	ctx := context.Background()
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer client.Close()

	subscription := client.Subscription("gcf-k8s-events-alert-export-k8s-events")
	fmt.Println("starting to recieve messages...")
	err = subscription.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
		fmt.Printf("Received message : %s\n", msg.Data)
		// fmt.Println("ERROR WITH RECIEVING MESSAGE:", err)
		var jsonMessage PubSubMessage
		json.Unmarshal(msg.Data, &jsonMessage)
		// fmt.Println("jsonMessage", jsonMessage)
		if jsonMessage.JsonPayload.Reason == "OOMKilling" {
			errCh := make(chan error, 1)
			go func() {
				errCh <- recoverOOM(jsonMessage)
			}()
			err := <-errCh
			if err != nil {
				fmt.Println("Error:", err)
			}
		}
		msg.Ack()
	})
	if err != nil {
		fmt.Println("stacked err", err)
	}
}

func recoverOOM(jsonMessage PubSubMessage) error {

	projectID := string(jsonMessage.Resource.Labels.Project_Id)
	functionRegion := string(jsonMessage.Resource.Labels.Location)
	clusterName := string(jsonMessage.Resource.Labels.Cluster_Name)

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
		return err
	}
	return nil
}

// func recoverOOM(projectID, location, clusterName string) error {
// 	cmd := exec.Command("go", "run", "../cmd/main.go", projectID, location, clusterName)
// 	cmd.Stdout = os.Stdout
// 	cmd.Stderr = os.Stderr
// 	err := cmd.Run()
// 	if err != nil {
// 		fmt.Printf("Failed to run processor : %v\n", err)
// 		return err
// 	} else {
// 		fmt.Println("Successfuly ran processor")
// 	}
// 	return nil
// }
