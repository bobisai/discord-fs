# discord-fs

A fuse filesystem using discord as storage backend because why not

## How to use

To mount you have to give discord-fs a bot token

# INFO BELOW IS OUTDATED WILL UPDATE IN FUTURE

## Speed 

disord-fs tries to encode 4 bytes into per character so with discord's 2k charcacter limit thats 8k bytes per message

The expected write speed is then 8k * the ratelimit
And the read speed is 8k*100 * ratelimit

Editing a file is a bit trickier, if increase the amount of messages required to store this file and its not the last one in a channel, it will have to delete all the original messages for the file and recreate them at the bottom, aswell as update all references to this file

## The index 

FileDescriptors are json encoded for readability and ease of use (Probably move to sumething like flatbuffers later)

It's seperated into folders, which contains entries

An entry consists of a `name`, `start message id`, `channel id`, `folder flag` and `count`

Start message id and channel id is used to reference the start message, where the file begins
Count is how many messages this file spans over
and folder flag is set if the file is a folder, channel id may also be ignored then since all folders are contained in the index channel

Each folder a always has 2 entries (excpept for the root folder, only has 1)
which is the current folder's entry, and the parent folder's entry, in that order