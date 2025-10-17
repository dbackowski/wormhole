# Wormhole 🕳️

A simple HTTP tunneling tool that exposes your local development server to the internet through custom subdomains.

## What it does

Wormhole creates a secure tunnel between your local development server and the internet, allowing you to:

- Share your local web application with others instantly
- Test webhooks and APIs that require public URLs
- Demo your work-in-progress applications
- Access your local development environment from anywhere

## Quick Start

### 1. Start the server
```bash
go run server/main.go
```

### 2. Connect your local app
```bash
go run client/main.go -domain=myapp -local=http://localhost:3000
```

### 3. Access your app
Your local server is now available at: `http://myapp.localhost:8080`

## Installation

```bash
git clone <repository-url>
cd wormhole
go mod download
```

## Usage

### Server Options
```bash
go run server/main.go -port=8080
```

### Client Options
```bash
go run client/main.go -server=localhost:8080 -domain=mysubdomain -local=http://localhost:3000
```

**Required flags:**
- `-domain`: Your unique subdomain name
- `-local`: URL of your local development server

**Optional flags:**
- `-server`: Server address (default: localhost:8080)

## Features

✅ **Custom Subdomains** - Choose your own subdomain name  
✅ **Real-time Tunneling** - Instant request forwarding via WebSockets  
✅ **Header Preservation** - Complete HTTP headers are maintained  
✅ **Multiple Clients** - Support for multiple simultaneous tunnels  
✅ **Automatic Cleanup** - Domains are released when clients disconnect  

## Example Use Cases

- **Frontend Development**: Share your React/Vue/Angular app with team members
- **API Testing**: Test webhook endpoints from external services
- **Mobile Development**: Test your local API with mobile apps
- **Client Demos**: Show work-in-progress to clients without deployment

## How it works

1. The server listens for client connections and HTTP requests
2. Clients connect via WebSocket and claim a subdomain
3. HTTP requests to `subdomain.server:port` are forwarded to the client
4. The client forwards requests to your local server and returns responses

## Requirements

- Go 1.22.3 or later
- Available port for the server (default: 8080)
- Local development server to tunnel

## License

Released under the MIT License.

## Contributing

Feel free to open issues and submit pull requests to improve Wormhole!
