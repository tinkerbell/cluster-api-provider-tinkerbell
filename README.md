![](https://img.shields.io/badge/Stability-Experimental-red.svg)

# Cluster API Provider Tink

This repository is
[Experimental](https://github.com/packethost/standards/blob/main/experimental-statement.md)
meaning that it's based on untested ideas or techniques and not yet established
or finalized or involves a radically new and innovative style! This means that
support is best effort (at best!) and we strongly encourage you to NOT use this
in production.

---

Cluster API Provider Tinkerbell (CAPT) is the implementation of Cluster API
Provider for Tinkerbell.

## Goal

* It acts as a bridge between Cluster API (a Kubernetes sig-lifecycle project)
  and Tinkerbell
* It simplifies Kubernetes cluster management using Tinkerbell as underline
  infrastructure provider
* Create, update, delete Kubernetes clusters in a declarative fashion.

## Current state

7th December 2020 marks the first commit for this project, it starts as a
porting from CAPP (cluster api provider packet).

As it is now it is able to bootstrap single Control Plane instance clusters using hardware
managed by Tinkerbell. Support for upgrades and multi-instance Control Plane clusters is
still outstanding.

See [docs/README.md](docs/README.md) for more information on setting up a development
environment.

## Technical preview

This project is under active development and you should expect issues, pull
requests and conversation ongoing in the [bi-weekly community
meeting](https://github.com/tinkerbell/.github/blob/main/COMMUNICATION.md#contributors-mailing-list).
Feel free to join if you are curious or if you have any question.

There is a milestone called `v0.1.0 tech preview`. Have a look at issues
assigned to that label to know more about what it will contain.
