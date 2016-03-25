containerops/builder
=============

A docker image that builds, tests and pushes docker images from Dockerfile, tarball or git repositories.

# Usage

## Build the image

The docker image - containerops/builder can be generated as the way below:

```bash
git clone https://github.com/ChengTiesheng/build-test.git
cd build-test
make
make image
```

## Use the image

### Build from Dockerfile

Run the following docker command:

	docker run --rm -it --privileged -e IMAGE_NAME=$IMAGE_NAME -e USERNAME=$USERNAME -e PASSWORD=$PASSWORD -e EMAIL=$EMAIL -e DOCKERFILE_URL=$DOCKERFILE_URL containerops/builder

Where:

* `$DOCKERFILE_URL` is the URL to the `Dockerfile` is to download and build.
* `$IMAGE_NAME` is the name of the image to create with an optional tag, i.e. `user/image-test:latest`
* `$USERNAME` is the username to use to log into the registry using `docker login`
* `$PASSWORD` is the password to use to log into the registry using `docker login`
* `$EMAIL` (optional) is the email to use to log into the registry using `docker login`

## Build from Git repository

Run the following docker command:

	docker run --rm -it --privileged -e IMAGE_NAME=$IMAGE_NAME -e GIT_USER=$GIT_USER -e GIT_REPO=$GIT_REPO -e GIT_TAG=$GIT_TAG -e USERNAME=$USERNAME -e PASSWORD=$PASSWORD -e EMAIL=$EMAIL containerops/builder 

Where:

* `$GIT_USER` is the user name of git hub, i.e. `huawei-openlab` in `https://github.com/huawei-openlab/oci2aci`
* `$GIT_REPO` is the git repository to clone and build, i.e. `oci2aci` in `https://github.com/huawei-openlab/oci2aci`
* `$GIT_TAG` (optional, defaults to `master`) is the tag/branch/commit to checkout after clone, i.e. `master`
* `$IMAGE_NAME` is the name of the image to create with an optional tag, i.e. `user/image-test:latest`
* `$USERNAME` is the username to use to log into the registry using `docker login`
* `$PASSWORD` is the password to use to log into the registry using `docker login`
* `$EMAIL` (optional) is the email to use to log into the registry using `docker login`

## Build from compressed tarball

Run the following docker command:

	docker run --rm -it --privileged -e IMAGE_NAME=$IMAGE_NAME -e TGZ_URL=$TGZ_URL -e USERNAME=$USERNAME -e PASSWORD=$PASSWORD -e EMAIL=$EMAIL containerops/builder

Where:

* `$TGZ_URL` is the URL to the compressed tarball (.tgz) to download and build.
* `$IMAGE_NAME` is the name of the image to create with an optional tag, i.e. `user/image-test:latest`
* `$USERNAME` is the username to use to log into the registry using `docker login`
* `$PASSWORD` is the password to use to log into the registry using `docker login`
* `$EMAIL` (optional) is the email to use to log into the registry using `docker login`
