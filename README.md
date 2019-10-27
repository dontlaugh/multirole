# multirole

Assume multiple AWS IAM roles at once with a single command

## How it works

This tool reads and writes your aws shared credentials file, usually located at
**~/.aws/credentials**. It's meant for use by humans, not machines.

It assumes your IAM is set up in a particular way:

* Your human user has static keys, but these don't let you do much, so you must
  assume roles to use the AWS API
* Your human user obtains a session token with static keys + a virtual MFA challenge
* Once a session is obtained, that MFA-protected session is able to assume roles
  without additional MFA challenges

So the flow looks like this

```
[identity keys] -> [GetSessionToken + MFA challenge] ---> [AssumeRole A]
                                                     `--> [AssumeRole B]
                                                     `--> [AssumeRole C]
```

We use a configuration file to specify which roles we want to assume, and our
MFA device. We can then write out all the temporary sessions to the shared credentials
file in a single pass.

## Getting Started

Back up your existing credentials.

```
cp ~/.aws/credentials ~/.aws/credentials.bak
```

Update the ~/.aws/credentials file to look like this (with your human user's 
real static keys, of course).

```ini
[identity]
aws_access_key_id = xxxxxxxx
aws_secret_access_key = xxxxxxxx
```

Create a config for the `multirole` tool. This location can be overridden with
a flag.

```
mkdir -p ~/.config/multirole
vi ~/.config/multirole/config.toml
```

The config is a TOML file that looks like the following

```toml
identity_profile = "identity"
mfa_serial = arn:aws:iam::55555555555:mfa/youruser

[[profile]]
name = "profile_a"
arn = "arn:aws:iam::66666666666:role/some_role"

[[profile]]
name = "profile_b"
arn = "arn:aws:iam::77777777777:role/some_other_role"

# more [[profile]] stanzas can be supplied...
```

The `identity_profile` must correspond to the stanza in the credentials file
that holds your human keys. The `mfa_serial` is an ARN for a Virtual MFA device
associated with your user.

## Installation

Requires Go 1.13+.

```
go get github.com/dontlaugh/multirole
```

