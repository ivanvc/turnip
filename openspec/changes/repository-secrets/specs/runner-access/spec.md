## ADDED Requirements

### Requirement: The repository's Secret
A repository's Secret SHALL be the Kubernetes Secret named `turnip-repository-<id>` in the Runner namespace (`TURNIP_K8S_NAMESPACE`), where `<id>` is the repository's numeric GitHub id as the webhook reports it, never a value read from `turnip.yaml`. A repository's Runner Pods SHALL mount keys only from its own repository's Secret, and only the keys each Project lists (see the `configuration` and `runner-job` capabilities). The operator SHALL create and fill the Secret; no Server setting SHALL gate it, so a repository whose Secret exists may mount any of its keys. turnip SHALL NOT read, create, modify or delete any Secret through the Kubernetes API (the kubelet mounts the keys), and the Server SHALL need no permission on Secrets.

#### Scenario: A Project lists a key
- **WHEN** repository id `123456789` has Project `lab` listing `lab-kubeconfig`
- **THEN** `lab`'s Runner Pod mounts key `lab-kubeconfig` of Secret `turnip-repository-123456789`, and no other key

#### Scenario: A pull request cannot reach another Secret
- **WHEN** a pull request's `turnip.yaml` lists a key named `private-key`
- **THEN** only its own repository's Secret is mounted from, so neither the `turnip-github-app` Secret nor another repository's Secret is ever mounted

#### Scenario: A renamed repository
- **WHEN** a repository is renamed or transferred
- **THEN** its Secret's name is unchanged

## MODIFIED Requirements

### Requirement: Access turnip does not provide
turnip SHALL NOT create a ServiceAccount, Role, RoleBinding or any other RBAC for the Runner, SHALL NOT create, fill or rotate a repository's Secret, SHALL NOT generate, inject or rotate any cloud credential, and SHALL NOT handle container registry credentials. turnip SHALL leave a Runner's cloud and cluster access to the operator, who may provide it through any of: the identity of the ServiceAccount the Runner runs as (for example EKS Pod Identity, IRSA or GKE Workload Identity), keys of the repository's Secret, or what the tool's own committed configuration reaches (for example a kubeconfig whose user runs `aws eks get-token` with a role to assume). A Runner Pod SHALL pull images only through the node's own credentials or an `imagePullSecrets` attached to its ServiceAccount. The only RBAC `deploy/base` grants is the Server's own (see the `deployment` capability).

#### Scenario: Runner with no grants
- **WHEN** an operator installs `deploy/base` and creates no ServiceAccount or RBAC for the Runner
- **THEN** Runner Pods run as the namespace's `default` ServiceAccount with whatever that account already holds, and turnip adds nothing to it

#### Scenario: Private image pull
- **WHEN** an allowed custom image lives in a private registry
- **THEN** the Runner Pod can pull it only if the node's credentials or the Runner ServiceAccount's `imagePullSecrets` grant access

#### Scenario: A cluster the ServiceAccount cannot reach
- **WHEN** a Project targets a cluster that accepts only a kubeconfig with an embedded credential
- **THEN** the operator puts that kubeconfig in the repository's Secret, the Project lists its key and points `KUBECONFIG` at it, and turnip creates nothing
