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

type PassedParams struct {
	Image_name      string
	Username        string
	Password        string
	Email           string
	Dockerfile      string
	Tar_url         string
	TarFile         []byte
	Github_username string
	Github_reponame string
	Github_tag      string
}

type PushAuth struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	Serveraddress string `json:"serveraddress"`
	Email         string `json:"email"`
}

type JobID struct {
	JobIdentifier string
}

type JobStatus struct {
	Status string
}

type JobLogs struct {
	Logs string
}

type StreamCatcher struct {
	ErrorDetail ErrorCatcher `json:"errorDetail"`
	Stream      string       `json:"stream"`
}

type ErrorCatcher struct {
	Message string `json:"message"`
	Error   string `json:"error"`
}

func main() {
	Image_name := os.Getenv("IMAGE_NAME")
	Username := os.Getenv("USERNAME")
	Password := os.Getenv("PASSWORD")
	Email := os.Getenv("EMAIL")
	Dockerfile := os.Getenv("DOCKERFILE")
	Tar_url := os.Getenv("TGZ_URL")
	Github_username := os.Getenv("GIT_USER")
	Github_reponame := os.Getenv("GIT_REPO")
	Github_tag := os.Getenv("GIT_TAG")

	passedParams := PassedParams{
		Image_name:      Image_name,
		Username:        Username,
		Password:        Password,
		Email:           Email,
		Dockerfile:      Dockerfile,
		Tar_url:         Tar_url,
		Github_username: Github_username,
		Github_reponame: Github_reponame,
		Github_tag:      Github_tag,
	}

	BuildImageFromDockerfile(passedParams)
}

//Builds the image in a docker node.
func BuildImageFromDockerfile(passedParams PassedParams) {

	if Validate(passedParams) {
		BuildPushAndDeleteImage(passedParams)
	} else {
		//Params didn't validate.  Bad Request.
		log.Printf("Insufficient Information.  Must provide at least an Image Name and a Dockerfile/Tarurl.")
		return
	}

}

func BuildPushAndDeleteImage(passedParams PassedParams) {

	//Parse the image name if it has a . in it.  Differentiate between private and docker repos.
	//Will cut quay.io/ichaboddee/ubuntu into quay.io AND ichaboddee/ubuntu.
	//If there is no . in it, then splitImageName[0-1] will be nil.  Code relies on that for logic later.
	splitImageName := make([]string, 2)
	if strings.Contains(passedParams.Image_name, ".") {
		splitImageName = strings.SplitN(passedParams.Image_name, "/", 2)
	}

	//Create the post request to build.  Query Param t=image name is the tag.
	buildUrl := ("/v1.21/build?nocache=true&t=" + passedParams.Image_name)

	//Open connection to docker and build.  The request will depend on whether a dockerfile was passed or a url to a zip.
	dockerDial := Dial()
	dockerConnection := httputil.NewClientConn(dockerDial, nil)
	readerForInput, err := ReaderForInputType(passedParams)
	if err != nil {
		log.Println(err)
		return
	}
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
			log.Println("logs", logsString)

			CacheBuildError := "Failed: " + stream.ErrorDetail.Message

			log.Println("status", CacheBuildError)

			return
		}

		if stream.Stream != "" {
			buildLogsSlice := []byte(logsString)
			buildLogsSlice = append(buildLogsSlice, []byte(stream.Stream)...)
			logsString = string(buildLogsSlice)

			log.Println("logs", logsString)
		}
	}

	log.Println("Build successfully, push will execute!")

	//Update status in the cache, then start the push process.
	log.Println("status", "Pushing")

	pushUrl := ("/images/" + passedParams.Image_name + "/push")
	// pushConnection := httputil.NewClientConn(dockerDial, nil)
	pushReq, err := http.NewRequest("POST", pushUrl, nil)
	pushReq.Header.Add("X-Registry-Auth", StringEncAuth(passedParams, ServerAddress(splitImageName[0])))
	pushResponse, err := dockerConnection.Do(pushReq)

	log.Println(pushResponse)

	pushReader := bufio.NewReader(pushResponse.Body)
	if err != nil {
		log.Println("status", err)
		return
	}

	//Loop through.  Only concerned with catching the error.  Append it to logsString if it exists.
	for {
		//Breaks when there is nothing left to read.
		line, err := pushReader.ReadBytes('\r')
		if err != nil {
			break
		}
		line = bytes.TrimSpace(line)

		//Unmarshal the json in to my structure.
		var stream StreamCatcher
		err = json.Unmarshal(line, &stream)

		//This if catches the error from docker and puts it in logs and status in the cache, then fails.
		if stream.ErrorDetail.Message != "" {
			pushLogsSlice := []byte(logsString)
			pushLogsSlice = append(pushLogsSlice, []byte(stream.ErrorDetail.Message)...)
			logsString = string(pushLogsSlice)
			log.Println("logs", logsString)
			CachePushError := "Failed: " + stream.ErrorDetail.Message
			log.Println("status", CachePushError)
			return
		}
	}

	//Finished.  Update status in the cache and close.
	log.Println("status", "Finished")

	//Delete it from the docker node.  Save space.
	deleteUrl := ("/v1.21/images/" + passedParams.Image_name)
	deleteReq, err := http.NewRequest("DELETE", deleteUrl, nil)
	dockerConnection.Do(deleteReq)
	dockerConnection.Close()
}

func Validate(passedParams PassedParams) bool {
	//Must have an image name and either a Dockerfile or TarUrl.
	switch {
	case passedParams.Dockerfile == "" && passedParams.Tar_url == "" && passedParams.TarFile == nil && passedParams.Github_reponame == "":
		return false
	case passedParams.Image_name == "":
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
	case passedParams.TarFile != nil:
		return bytes.NewReader(passedParams.TarFile), nil
	case passedParams.Tar_url != "":
		return ReaderForTarUrl(passedParams.Tar_url)
	case passedParams.Github_tag != "" && passedParams.Github_username != "" && passedParams.Github_reponame != "":
		return ReaderForGithubTar(passedParams)
	default:
		return nil, errors.New("Failed in the ReaderForInputType.  Got to default case.")
	}

}

func ReaderForGithubTar(passedParams PassedParams) (*bytes.Buffer, error) {

	url := "https://github.com/" + passedParams.Github_username + "/" + passedParams.Github_reponame + "/archive/" + passedParams.Github_tag + ".tar.gz"
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
	matchedv, err := regexp.MatchString("^v", passedParams.Github_tag)
	if matchedv {
		githubTagSlice := strings.SplitAfterN(passedParams.Github_tag, "v", 2)
		folderName = passedParams.Github_reponame + "-" + githubTagSlice[1] + "/"
	} else {
		folderName = passedParams.Github_reponame + "-" + passedParams.Github_tag + "/"
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

	log.Println("Get Tarfile", response.Body)

	return response.Body, nil
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
