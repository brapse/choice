package main

import (
	"bytes"
	"context"
	"encoding/json"

	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"time"

	firebase "firebase.google.com/go"
	"github.com/mitchellh/hashstructure/v2"
	// "cloud.google.com/go/bigquery"
)

// Item represents a row item. the auction is initially just open or closed, but later on different kinds of openings (time bundled, solo, etc)
type LogEntry struct {
	paramsdHash string
	Payload     map[string]interface{}
	Auction     string
	timestamp   time.Time
}

/*
	Logging
*/

// Save implements the ValueSaver interface.
// This example disables best-effort de-duplication, which allows for higher throughput.
func saveLogItem(logItem LogEntry) {

	// save the log entry - right now we are just saving it to the log

	if reqHeadersBytes, err := json.MarshalIndent(logItem, "", "\t"); err != nil {
		log.Println("Could not Marshal Req Headers")
	} else {
		log.Println(string(reqHeadersBytes))
	}

	projectID := "choice-operator"

	// Use the application default credentials
	ctx := context.Background()
	conf := &firebase.Config{ProjectID: projectID}
	app, err := firebase.NewApp(ctx, conf)
	if err != nil {
		log.Fatalln(err)
	}

	client, err := app.Firestore(ctx)
	if err != nil {
		log.Fatalln(err)
	}
	defer client.Close()
	// Create fails if the document exists, th ebehaviour we want
	_, err = client.Collection("txs").Doc(logItem.paramsHash).Create(ctx, logItem)
	if err != nil {
		log.Fatalf("Failed adding alovelace: %v", err)
	}

}

/*
	Getters
*/

// Get the url for a given proxy condition
func getProxyUrl() string {

	// put logic in here that chooses the proxy

	default_condition_url := "https://eth-mainnet.alchemyapi.io/v2/ikJ14RMH8ZjS-H0F3QUOd-lwec5TzkcV/"

	return default_condition_url
}

// Parse the requests body
func parseRequestBody(request *http.Request) map[string]interface{} {

	// Read body to buffer
	body, err := ioutil.ReadAll(request.Body)

	if err != nil {
		log.Printf("Error reading body: %v", err)
		panic(err)
	}

	var requestPayload map[string]interface{}
	err = json.Unmarshal([]byte(body), &requestPayload)

	if err != nil {
		log.Printf("Error reading body: %v", err)
		panic(err)
	}

	// Because go lang is a pain in the ass if you read the body then any susequent calls
	// are unable to read the body again....
	request.Body = ioutil.NopCloser(bytes.NewBuffer(body))

	return requestPayload
}

// Given a request send it to the appropriate url
func handleRequestAndRedirect(res http.ResponseWriter, req *http.Request) {
	requestPayload := parseRequestBody(req)

	if requestPayload["method"] == "eth_sendRawTransaction" || requestPayload["method"] == "eth_sendTransaction" || requestPayload["method"] == "eth_sendRawTransaction_reserve" || requestPayload["method"] == "eth_sendTransaction_reserve" {
		// this we want to keep, build and save log
		objectHash, err := hashstructure.Hash(requestPayload["params"], hashstructure.FormatV2, nil)
		objectHashString := strconv.FormatUint(objectHash, 10)

		if err != nil {
			log.Panicf("%d", err)
		}
		logItem := LogEntry{paramsHash: objectHashString, Payload: requestPayload, timestamp: time.Now(), Auction: "open"}
		//TODO; check im not overwritting somehting (could be malicious)
		saveLogItem(logItem)

		res.Header().Set("X-Choice-Operator-Version", "0.01")
		res.Header().Set("Content-Type", "application/json")

		// response should be  { “id”:1, “jsonrpc”: “2.0”, “result”: “” })
		jsonResponse := "{\"id\":1, \"jsonrpc\": \"2.0\", \"result\": \"\"}"

		fmt.Fprintf(res, jsonResponse)

	} else {
		//foward to infura/alchemy/whatever our default it; do i need th eheaders i am not logging? Headers: req.Header,
		// parse the url
		target := getProxyUrl()
		url, _ := url.Parse(target)
		// create the reverse proxy
		proxy := httputil.NewSingleHostReverseProxy(url)

		// Update the headers to allow for SSL redirection
		req.URL.Host = url.Host
		req.URL.Scheme = url.Scheme
		// req.Header.Set("X-Forwarded-Host", req.Header.Get("Host"))
		req.Header.Set("X-Choice-Operator-Version", "0.01")
		req.Host = url.Host

		// Note that ServeHttp is non blocking and uses a go routine under the hood
		proxy.ServeHTTP(res, req)
	}

}

func debugHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "debugging")
}

/*
	Entry
*/

func main() {
	log.Print("starting server...")

	// start server
	http.HandleFunc("/", handleRequestAndRedirect)
	// start server
	http.HandleFunc("/debug", debugHandler)

	// Determine port for HTTP service.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("defaulting to port %s", port)
	}

	// Start HTTP server.
	log.Printf("listening on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
