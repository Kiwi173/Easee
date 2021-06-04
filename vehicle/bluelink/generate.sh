#!/bin/sh
docker pull hacksore/hks
docker run --rm hacksore/hks kia list 693a33fa-c117-43f2-ae3b-61a02d24f417 | head -n 101 | tail -n 100 > 693a33fa-c117-43f2-ae3b-61a02d24f417
docker run --rm hacksore/hks hyundai list 99cfff84-f4e2-4be8-a5ed-e5b755eb6581 | head -n 101 | tail -n 100 > 99cfff84-f4e2-4be8-a5ed-e5b755eb6581
