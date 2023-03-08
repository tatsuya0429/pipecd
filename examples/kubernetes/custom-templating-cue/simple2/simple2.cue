package kube

deployment: simple2: {
	apiVersion: "apps/v1"
	kind:       "Deployment"
	metadata: {
		name: "simple2"
		labels: app: "simple2"
	}
	spec: {
		replicas: 2
		selector: matchLabels: {
			app:                  "simple2"
			"pipecd.dev/variant": "primary"
		}
		template: {
			metadata: {
				labels: {
					app:                  "simple2"
				}
			}
			spec: containers: [{
				name:  "helloworld"
				image: "ghcr.io/pipe-cd/helloworld:v0.32.0"
				args: [
					"server",
				]
				ports: [{
					containerPort: 9085
				}]
			}]
		}
	}
}
service: simple2: {
	apiVersion: "v1"
	kind:       "Service"
	metadata: name: "simple2"
	spec: {
		selector: app: "simple2"
		ports: [{
			protocol:   "TCP"
			port:       9085
			targetPort: 9085
		}]
	}
}