#!/bin/bash

dpkg -i --force-depends /home/ag/cri-dockerd_0.3.7.3-0.debian-buster_amd64.deb
systemctl start cri-docker.service