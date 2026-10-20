## ADDED Requirements

### Requirement: The Runner namespace holds nothing of the Server's
Runner Jobs SHALL run in the Runner namespace: `TURNIP_RUNNER_NAMESPACE`, or the Server's own namespace when it is unset (see the `server` capability). The deployment base SHALL leave it unset, so a first install needs one namespace; the operator documentation SHALL recommend separating the Runner namespace (see the `deployment` capability) and state that, until then, Runners run beside the Server, where a Runner's grant on Secrets or Pods reaches the GitHub App's private key and the Server Pod. When the two namespaces differ, the Runner namespace SHALL hold no object of the Server's: not its Deployment, Pods, ServiceAccount, ConfigMap or the `turnip-github-app` Secret. What the Server needs there SHALL be granted to its ServiceAccount from the Server namespace (see the `deployment` capability). A Runner ServiceAccount, its grants and the repository's Secret SHALL live in the Runner namespace, created by the operator.

#### Scenario: A Runner granted every Secret in its namespace
- **WHEN** an operator grants a Runner ServiceAccount `get` on all Secrets in the Runner namespace, and a pull request's code uses it
- **THEN** it can read the repository's Secrets, but not `turnip-github-app`, which is in the Server namespace

#### Scenario: A Runner granted every Pod in its namespace
- **WHEN** a Runner ServiceAccount may `exec` into any Pod in the Runner namespace
- **THEN** no Server Pod is among them

#### Scenario: Runners beside the Server
- **WHEN** `TURNIP_RUNNER_NAMESPACE` is unset and the Server runs in `turnip-system`
- **THEN** Runner Jobs run in `turnip-system`, and a Runner ServiceAccount granted `get` on its namespace's Secrets can read `turnip-github-app`

## MODIFIED Requirements

### Requirement: The repository's Secret
A repository's Secret SHALL be the Kubernetes Secret named `turnip-repository-<id>` in the Runner namespace, where `<id>` is the repository's numeric GitHub id as the webhook reports it, never a value read from `turnip.yaml`. A repository's Runner Pods SHALL mount keys only from its own repository's Secret, and only the keys each Project lists (see the `configuration` and `runner-job` capabilities). The operator SHALL create and fill the Secret; no Server setting SHALL gate it, so a repository whose Secret exists may mount any of its keys. turnip SHALL NOT read, create, modify or delete any Secret through the Kubernetes API (the kubelet mounts the keys), and the Server SHALL need no permission on Secrets.

#### Scenario: A Project lists a key
- **WHEN** repository id `123456789` has Project `lab` listing `lab-kubeconfig`
- **THEN** `lab`'s Runner Pod mounts key `lab-kubeconfig` of Secret `turnip-repository-123456789`, and no other key

#### Scenario: A pull request cannot reach another Secret
- **WHEN** a pull request's `turnip.yaml` lists a key named `private-key`
- **THEN** only its own repository's Secret is mounted from, so neither the `turnip-github-app` Secret nor another repository's Secret is ever mounted

#### Scenario: A renamed repository
- **WHEN** a repository is renamed or transferred
- **THEN** its Secret's name is unchanged
