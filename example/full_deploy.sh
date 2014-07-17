#!/bin/bash

go build flkadmin.go    &&

./flkadmin -add_node 1 -availability_zone us-west-2a  &&
./flkadmin -add_node 1 -availability_zone us-west-2b  &&
./flkadmin -add_node 1 -availability_zone us-west-2c  &&
./flkadmin -setup                                     &&
./flkadmin -deploy                                    &&
./flkadmin -run "curl -s http://localhost/id; echo"