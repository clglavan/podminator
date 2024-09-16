
# Podminator

Podminator is a terminal-based Kubernetes pod management tool built using the `tview` and `client-go` libraries. It allows you to easily interact with Kubernetes pods, including viewing logs, executing commands, fetching pod details, and switching between namespaces, all from within a terminal-based user interface. With support for multi-container pods and toggle-able terminal output, Podminator streamlines the pod management experience for Kubernetes users.

## Features

- **Logs Viewer:** Quickly view the logs for your Kubernetes pods or specific containers.
- **Exec into Pods:** Open a shell session directly inside a running container.
- **Tail Logs in Real-Time:** Follow pod logs as they are generated.
- **Pod Information:** Retrieve YAML and describe output for pods.
- **Namespace Switching:** Easily switch between different namespaces.
- **UI Output or Terminal:** Toggle between displaying command output in the terminal UI or a new terminal window.
- **Multi-container Pods:** Support for pods with multiple containers, allowing you to choose which container to interact with.
- **Auto refresh Pods:** Automatically refresh pods every 5 seconds
- 
Coming soon:
- **Support for extra resources:** Allow see and edit extra resources like deployment, configmap, secrets, pvc, volumes, HPA, Ingress.
- **Special commands:** Debug running pods by cloning them and debug by cloning the image locally.

## Building

### Prerequisites

- Go (version 1.22)
- Access to a Kubernetes cluster
- `kubectl` and a valid kubeconfig file

### Clone the Repository

```bash
git clone https://github.com/clglavan/podminator.git
cd podminator
```

### Build the Project

```bash
go build -o podminator main.go
```

Or use the [Makefile](Makefile)

### Run the Project

```bash
./podminator
```

By default, Podminator will use the kubeconfig file located in `~/.kube/config`. You can also specify a custom kubeconfig file using the `--kubeconfig` flag.

```bash
./podminator --kubeconfig /path/to/your/kubeconfig
```

## Usage

Once you run the `podminator` executable, you will see a terminal user interface with the following layout:

1. **Helper Text:** This section at the top provides quick shortcuts and options for interacting with your Kubernetes pods.
2. **Search Field:** Allows you to filter the pods by name.
3. **Namespace Dropdown:** Select different namespaces to view the pods running in those namespaces.
4. **Pod List:** Displays the list of pods based on the selected namespace and search query.
5. **Command Output Section:** Shows the output of your selected command (logs, describe, etc.).

### Keyboard Shortcuts

| Key           | Action                                  |
|---------------|-----------------------------------------|
| `o`           | Toggle between terminal output and UI output |
| `l`           | View pod logs                           |
| `t`           | Tail logs in real-time (new terminal)   |
| `e`           | Execute a shell command in a pod        |
| `E` (Shift+e) | Open modal, enter custom command for exec |
| `i`           | Show detailed pod information (describe) |
| `y`           | Show pod YAML                           |
| `n`           | Switch between namespaces               |
| `s`           | Focus on the search input field         |
| `q`           | Quit the application                    |
| Arrow Keys    | Navigate between sections               |

### Multi-Container Pods

For pods with multiple containers, Podminator presents a modal allowing you to choose which container to interact with. You can navigate through the container options using the arrow keys and select a container with the Enter key.

### Toggle Terminal Output

By default, Podminator displays some command output directly in the UI (like describe, logs, yaml). However, you can toggle between UI output and opening a new terminal window for commands using the `o` key.

For exec and tail commands (ones that require an interactive input) the output will always be send to a new native terminal window.

## Development

If you want to contribute to Podminator, follow these steps:

1. Fork the repository.
2. Make your changes in a new branch.
3. Submit a pull request with a description of your changes.

### Running in Development Mode

During development, you can run the project directly using Go:

```bash
go run main.go
```

## Example Use Case

Let's say you want to view logs for a pod in the `default` namespace:

1. Select the `default` namespace from the dropdown.
2. Use the search input to filter pods by name.
3. Highlight the pod you are interested in.
4. Press `l` to view the logs.
5. If the pod has multiple containers, choose the container from the modal.

Alternatively, you can press `t` to tail the logs and see real-time updates from your container.

## Troubleshooting

### Common Issues

- **No Pods Listed:** Ensure your kubeconfig is properly set and you have access to the cluster.
- **Modal Not Responding:** When using modals, ensure to press the appropriate keys for navigation (`Enter` to select and arrow/tab keys to move between options).

### Logs

To help troubleshoot issues, Podminator prints error logs to the terminal running the application. These logs provide insights into any errors encountered while interacting with Kubernetes (such as invalid commands or permission issues).

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
