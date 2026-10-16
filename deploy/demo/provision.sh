#!/bin/sh
# provision.sh - installs or updates the public demonstration, https://prova.perfuse.health:9443, from this machine.
#
# It runs beside Perfuse's public test instance and Vaglio's demonstration on the same Amazon Lightsail nano: the DNS
# record in the perfuse.health zone, port 9443 opened, and setup.sh run there. The binary is built here from this
# checkout. Run again to upgrade; the certificate and the database are kept.
#
# Needs the AWS CLI signed in to the account that holds the perfuse.health zone. The SSH key is kept in $KEYS (default
# ~/keys), mode 600, and never printed.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
export AWS_PAGER=""
region=us-east-1
name=perfuse-test
port=9443
host=prova.perfuse.health
keys=${KEYS:-$HOME/keys}
key="$keys/lightsail-$region.pem"

build=$(mktemp -d)
trap 'rm -rf "$build"' EXIT
(cd "$root" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w -buildid= -X main.version=demo-$(git rev-parse --short HEAD)" -o "$build/prova" ./cmd/prova)

if ! aws lightsail get-instance --region $region --instance-name $name >/dev/null 2>&1; then
  echo "$name does not exist: create it with Perfuse's deploy/test-instance/provision.sh first" >&2
  exit 1
fi
ip=$(aws lightsail get-static-ip --region $region --static-ip-name $name-ip --query staticIp.ipAddress --output text)
aws lightsail open-instance-public-ports --region $region --instance-name $name \
  --port-info "fromPort=$port,toPort=$port,protocol=tcp" >/dev/null

zone=$(aws route53 list-hosted-zones-by-name --dns-name perfuse.health --query 'HostedZones[0].Id' --output text)
aws route53 change-resource-record-sets --hosted-zone-id "$zone" --change-batch "{\"Changes\":[{\"Action\":\"UPSERT\",
  \"ResourceRecordSet\":{\"Name\":\"$host\",\"Type\":\"A\",\"TTL\":300,\"ResourceRecords\":[{\"Value\":\"$ip\"}]}}]}" >/dev/null
until [ "$(dig +short $host @8.8.8.8 | tail -1)" = "$ip" ]; do sleep 10; done
echo "$host is $ip"

ssh_opts="-i $key -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=$keys/known_hosts -o ConnectTimeout=10"
ssh $ssh_opts admin@"$ip" 'rm -rf /tmp/prova-demo && mkdir -p /tmp/prova-demo'
scp -q $ssh_opts "$build/prova" "$here/setup.sh" "$here/prova-demo.service" admin@"$ip":/tmp/prova-demo/
ssh $ssh_opts admin@"$ip" "sudo sh /tmp/prova-demo/setup.sh"
