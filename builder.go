package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"regexp"
	"strings"
)

const (
	// APIVersion represents docker remote API version.
	APIVersion = "v1.21"
)

// PassedParams contains the parameters needed by docker build.
type PassedParams struct {
	ImageName      string
	Username       string
	Password       string
	Email          string
	Dockerfile     string
	Dockerfile_url string
	Tar_url        string
	TarFile        []byte
	GitUsr         string
	GitRepo        string
	GitTag         string
}

// PushAuth
type PushAuth struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	Serveraddress string `json:"serveraddress"`
	Email         string `json:"email"`
}

// StreamCatcher
type StreamCatcher struct {
	ErrorDetail ErrorCatcher `json:"errorDetail"`
	Stream      string       `json:"stream"`
}

// ErrorCatcher
type ErrorCatcher struct {
	Message string `json:"message"`
	Error   string `json:"error"`
}

// MessageStream
type MessageStream struct {
	Error       string `json:"error"`
	ErrorDetail struct {
		Message string `json:"message"`
	} `json:"errorDetail"`
	ID             string `json:"id"`
	Progress       string `json:"progress"`
	ProgressDetail struct {
		Current int `json:"current"`
		Total   int `json:"total"`
	} `json:"progressDetail"`
	Status string `json:"status"`
	Stream string `json:"stream"`
}

func main() {
	ImageName := os.Getenv("IMAGE_NAME")
	Username := os.Getenv("USERNAME")
	Password := os.Getenv("PASSWORD")
	Email := os.Getenv("EMAIL")
	Dockerfile := os.Getenv("DOCKERFILE")
	Dockerfile_url := os.Getenv("DOCKERFILE_URL")
	Tar_url := os.Getenv("TGZ_URL")
	GitUsr := os.Getenv("GIT_USER")
	GitRepo := os.Getenv("GIT_REPO")
	GitTag := os.Getenv("GIT_TAG")

	passedParams := PassedParams{
		ImageName:      ImageName,
		Username:       Username,
		Password:       Password,
		Email:          Email,
		Dockerfile:     Dockerfile,
		Dockerfile_url: Dockerfile_url,
		Tar_url:        Tar_url,
		GitUsr:         GitUsr,
		GitRepo:        GitRepo,
		GitTag:         GitTag,
	}

	BuildImage(passedParams)
}

//Builds the image in a docker node.
func BuildImage(passedParams PassedParams) {

	if Validate(passedParams) {
		BuildPushAndDeleteImage(passedParams)
	} else {
		//Params didn't validate.  Bad Request.
		log.Printf("Insufficient Information. Must provide at least an Image Name and a Dockerfile/Tarurl.")
		return
	}

}

func BuildPushAndDeleteImage(passedParams PassedParams) {

	//Parse the image name if it has a . in it.  Differentiate between private and docker repos.
	//Will cut quay.io/ichaboddee/ubuntu into quay.io AND ichaboddee/ubuntu.
	//If there is no . in it, then splitImageName[0-1] will be nil.  Code relies on that for logic later.
	splitImageName := make([]string, 2)
	if strings.Contains(passedParams.ImageName, ".") {
		splitImageName = strings.SplitN(passedParams.ImageName, "/", 2)
	}

	//Create the post request to build.  Query Param t=image name is the tag.
	buildUrl := ("/v1.21/build?nocache=true&t=" + passedParams.ImageName)

	//Open connection to docker and build.  The request will depend on whether a dockerfile was passed or a url to a zip.
	dockerDial := Dial()
	dockerConnection := httputil.NewClientConn(dockerDial, nil)
	readerForInput, err := ReaderForInputType(passedParams)
	if err != nil {
		log.Println(err)
		return
	}

	log.Println("Status", "Building")

	buildReq, err := http.NewRequest("POST", buildUrl, readerForInput)
	buildResponse, err := dockerConnection.Do(buildReq)

	defer buildResponse.Body.Close()
	buildReader := bufio.NewReader(buildResponse.Body)
	if err != nil {
		log.Println("Building image returns error! The job terminated!")
		return
	}

	var logsString string
	//Loop through.  If stream is there append it to the logsString and update the cache.
	for {
		//Breaks when there is nothing left to read.
		line, err := buildReader.ReadBytes('\r')
		if err != nil {
			break
		}
		line = bytes.TrimSpace(line)

		//Unmarshal the json in to my structure.
		var stream StreamCatcher
		err = json.Unmarshal(line, &stream)

		//This if catches the error from docker and puts it in logs in the cache, then fails.
		if stream.ErrorDetail.Message != "" {

			buildLogsSlice := []byte(logsString)
			buildLogsSlice = append(buildLogsSlice, []byte(stream.ErrorDetail.Message)...)
			logsString = string(buildLogsSlice)

			log.Println("Build log:\n", logsString)

			CacheBuildError := "Failed: " + stream.ErrorDetail.Message

			log.Println("Build error:\n", CacheBuildError)

			return
		}

		if stream.Stream != "" {
			buildLogsSlice := []byte(logsString)
			buildLogsSlice = append(buildLogsSlice, []byte(stream.Stream)...)
			logsString = string(buildLogsSlice)
		}
	}

	log.Println("Build log:\n", logsString)
	log.Println("Build successfully, push start!")

	//Update status in the cache, then start the push process.
	log.Println("Status", "Pushing")

	pushUrl := ("/images/" + passedParams.ImageName + "/push")
	// pushConnection := httputil.NewClientConn(dockerDial, nil)
	pushReq, err := http.NewRequest("POST", pushUrl, nil)
	pushReq.Header.Add("X-Registry-Auth", StringEncAuth(passedParams, ServerAddress(splitImageName[0])))
	pushResponse, err := dockerConnection.Do(pushReq)

	pushReader := bufio.NewReader(pushResponse.Body)
	if err != nil {
		log.Println("Status", err)
		return
	}

	var logsStringPush string
	//Loop through.  Only concerned with catching the error.  Append it to logsString if it exists.
	for {
		//Breaks when there is nothing left to read.
		line, err := pushReader.ReadBytes('\r')
		if err != nil {
			break
		}
		line = bytes.TrimSpace(line)

		//Unmarshal the json in to my structure.
		var stream MessageStream
		err = json.Unmarshal(line, &stream)

		//This if catches the error from docker and puts it in logs and status in the cache, then fails.
		if stream.ErrorDetail.Message != "" {
			pushLogsSlice := []byte(logsStringPush)
			pushLogsSlice = append(pushLogsSlice, []byte(stream.ErrorDetail.Message)...)
			logsStringPush = string(pushLogsSlice)
			log.Println("Push log:\n", logsStringPush)
			CachePushError := "Failed: " + stream.ErrorDetail.Message
			log.Println("Push error:\n", CachePushError)
			return
		}

		if stream.ID != "" {
			pushLogsSlice := []byte(logsStringPush)
			pushLogsSlice = append(pushLogsSlice, []byte(stream.ID)...)
			logsStringPush = string(pushLogsSlice)
		}

		if stream.Progress != "" {
			pushLogsSlice := []byte(logsStringPush)
			pushLogsSlice = append(pushLogsSlice, []byte(stream.Progress)...)
			logsStringPush = string(pushLogsSlice)
		}

		if stream.Status != "" {
			pushLogsSlice := []byte(logsStringPush)
			pushLogsSlice = append(pushLogsSlice, []byte(stream.Status)...)
			logsStringPush = string(pushLogsSlice)
		}

		if stream.Stream != "" {
			pushLogsSlice := []byte(logsStringPush)
			pushLogsSlice = append(pushLogsSlice, []byte(stream.Stream)...)
			logsStringPush = string(pushLogsSlice)
		}
	}

	log.Println("Push log:\n", logsStringPush)

	//Finished.  Update status in the cache and close.
	log.Println("Status", "Finished")

	//Delete it from the docker node.  Save space.
	deleteUrl := ("/v1.21/images/" + passedParams.ImageName)
	deleteReq, err := http.NewRequest("DELETE", deleteUrl, nil)
	dockerConnection.Do(deleteReq)
	dockerConnection.Close()
}

func Validate(passedParams PassedParams) bool {
	Dockerfile := passedParams.Dockerfile
	Dockerfile_url := passedParams.Dockerfile_url
	Tar_url := passedParams.Tar_url
	TarFile := passedParams.TarFile
	GitRepo := passedParams.GitRepo

	//Must have an image name and either a Dockerfile or TarUrl.
	switch {
	case Dockerfile == "" && Dockerfile_url == "" && Tar_url == "" && TarFile == nil && GitRepo == "":
		return false
	case passedParams.ImageName == "":
		return false
	default:
		return true
	}
}

//String encode the info required for X-AUTH.  Username, Password, Email, Serveraddress.
func StringEncAuth(passedParams PassedParams, serveraddress string) string {
	//Encoder the needed data to pass as the X-RegistryAuth Header
	var data PushAuth
	data.Username = passedParams.Username
	data.Password = passedParams.Password
	data.Email = passedParams.Email
	data.Serveraddress = serveraddress

	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Println("error:", err)
	}
	sEnc := base64.StdEncoding.EncodeToString([]byte(jsonData))
	return sEnc
}

//Essentially docker_node := os.Getenv("DOCKER_NODE") | default_node
func DockerNode() string {

	var docker_node string
	if os.Getenv("DOCKER_HOST") != "" {
		docker_node = os.Getenv("DOCKER_HOST")
	} else {
		docker_node = "http://127.0.0.1:4243"
	}
	return docker_node
}

func Dial() net.Conn {
	var docker_proto string
	var docker_host string
	if os.Getenv("DOCKER_HOST") != "" {
		dockerHost := os.Getenv("DOCKER_HOST")
		splitStrings := strings.SplitN(dockerHost, "://", 2)
		docker_proto = splitStrings[0]
		docker_host = splitStrings[1]
	} else {
		docker_proto = "tcp"
		docker_host = "localhost:4243"
	}

	dockerDial, err := net.Dial(docker_proto, docker_host)
	if err != nil {
		log.Println("Failed to reach docker")
		log.Fatal(err)
	}
	return dockerDial
}

func ServerAddress(privateRepo string) string {

	//The server address is different for a private repo.
	var serveraddress string
	if privateRepo != "" {
		serveraddress = ("https://" + privateRepo + "/v1/")
	} else {
		serveraddress = "https://index.docker.io/v1/"
	}
	return serveraddress

}

//Reader will read from either the zip made from the dockerfile passed in or the zip from the url passed in.
func ReaderForInputType(passedParams PassedParams) (io.Reader, error) {

	//Use a switch.  one case for Dockerfile, one for TarUrl, one for Tarfile from client?
	switch {
	case passedParams.Dockerfile != "":
		return ReaderForDockerfile(passedParams.Dockerfile), nil
	case passedParams.Dockerfile_url != "":
		return ReaderForDockerfileUrl(passedParams.Dockerfile_url)
	case passedParams.TarFile != nil:
		return bytes.NewReader(passedParams.TarFile), nil
	case passedParams.Tar_url != "":
		return ReaderForTarUrl(passedParams.Tar_url)
	case passedParams.GitRepo != "":
		return ReaderForGitRepo(passedParams)
	default:
		return nil, errors.New("Failed in the ReaderForInputType.  Got to default case.")
	}

}

func ReaderForGitRepo(passedParams PassedParams) (*bytes.Buffer, error) {

	url := "https://github.com/" + passedParams.GitUsr + "/" + passedParams.GitRepo + "/archive/" + passedParams.GitTag + ".tar.gz"
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	response, err := client.Do(req)
	if err != nil {
		log.Fatalln(err)
	}
	//Fail Gracefully  If the response is a 404 send that back.
	if response.Status == "404 Not Found" {
		log.Println("status", response.Status, "Make sure the github repo and tag are correct.")
		return nil, errors.New("Failed download from github.")
	}
	contents, err := ioutil.ReadAll(response.Body)

	//Now let's unzip it.
	zippedBytesReader := bytes.NewReader(contents)
	gzipReader, err := gzip.NewReader(zippedBytesReader)

	var b bytes.Buffer
	io.Copy(&b, gzipReader)

	var folderName string
	//Name of the folder created by github.  We use for Regex and renaiming.  maybe ^v.+
	matchedv, err := regexp.MatchString("^v", passedParams.GitTag)
	if matchedv {
		githubTagSlice := strings.SplitAfterN(passedParams.GitTag, "v", 2)
		folderName = passedParams.GitRepo + "-" + githubTagSlice[1] + "/"
	} else {
		folderName = passedParams.GitRepo + "-" + passedParams.GitTag + "/"
	}

	//Final buffer will catch our new TarFile
	finalBuffer := new(bytes.Buffer)
	tarWriter := tar.NewWriter(finalBuffer)

	//Reads our unzipped TarFile so that we can loop through each file.
	tarBytesReader := bytes.NewReader(b.Bytes())
	tarReader := tar.NewReader(tarBytesReader)
	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			// end of tar archive
			break
		}
		if err != nil {
			log.Println(err)
		}

		//Regex will match the content in the folders path, and not the folder itself.
		//Then we can copy the file into our new tar file.
		matchedFolder, err := regexp.MatchString(folderName+".+", hdr.Name)
		if matchedFolder {
			unloadBuffer := new(bytes.Buffer)
			_, err := io.Copy(unloadBuffer, tarReader)
			if err != nil {
				log.Println(err)
			}

			//Strip the folder off the name.
			// change the header name
			slicedHeader := strings.SplitN(hdr.Name, folderName, 2)
			hdr.Name = slicedHeader[1]
			if err := tarWriter.WriteHeader(hdr); err != nil {
				log.Println(err)
			}

			if _, err := tarWriter.Write(unloadBuffer.Bytes()); err != nil {
				log.Println(err)
			}
		}
	}
	return finalBuffer, nil
}

//URL example = https://github.com/tutumcloud/docker-hello-world/archive/v1.0.tar.gz
func ReaderForTarUrl(url string) (io.ReadCloser, error) {

	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("Failed response from Tarball Url.")
	}

	log.Println("Get Tarfile successfully.")

	return response.Body, nil
}

//URL example = https://github.com/tutumcloud/docker-hello-world/archive/v1.0.tar.gz
func ReaderForDockerfileUrl(url string) (*bytes.Buffer, error) {

	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("Failed response from Dockerfile Url.")
	}

	contents, _ := ioutil.ReadAll(response.Body)

	dockerfile := string(contents)

	return ReaderForDockerfile(dockerfile), nil
}

func ReaderForDockerfile(dockerfile string) *bytes.Buffer {

	// Create a buffer to write our archive to.
	buf := new(bytes.Buffer)

	// Create a new tar archive.
	tw := tar.NewWriter(buf)

	// Add the dockerfile to the archive.
	var files = []struct {
		Name, Body string
	}{
		{"Dockerfile", dockerfile},
	}
	for _, file := range files {
		hdr := &tar.Header{
			Name: file.Name,
			Size: int64(len(file.Body)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			log.Fatalln(err)
		}
		if _, err := tw.Write([]byte(file.Body)); err != nil {
			log.Fatalln(err)
		}
	}
	//Check the error on Close.
	if err := tw.Close(); err != nil {
		log.Fatalln(err)
	}
	return buf
}
