FROM golang:1.17.5 AS builder

RUN mkdir /tmp/custom-tekton-listener

COPY . /tmp/custom-tekton-listener/

WORKDIR /tmp/custom-tekton-listener

RUN CGO_ENABLED=0 GOOS=linux go build -o custom-tekton-listener main.go

FROM centos:8

USER root

# Copy app from builder image
COPY --from=builder /tmp/custom-tekton-listener/custom-tekton-listener /usr/local/bin/

RUN chmod +x /usr/local/bin/custom-tekton-listener 

RUN yum update -y && \
    yum install -y curl vim wget tar && \
    yum clean all

# Installation tkn cli but custom-tekton-listener does not use it
#RUN cd /tmp && \
#    wget https://github.com/tektoncd/cli/releases/download/v0.21.0/tkn_0.21.0_Linux_x86_64.tar.gz && \
#    tar -zxvf tkn_0.21.0_Linux_x86_64.tar.gz && \
#    chmod +x tkn && \
#    mv tkn /usr/local/bin/ && \
#    rm -rf README.md LICENSE *.gz &> /dev/null
    
CMD ["/usr/local/bin/custom-tekton-listener"]