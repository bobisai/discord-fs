# discord-fs

A fuse filesystem using discord as storage device

This is my first filesystem, and its also using discord as the storage device because why have speed and reliability when you can have uh... holdon im getting a phone call...

## How to use

You will need a server and a bot token, discord-fs will use the default channel in your server.

discord-fs "token" "serverid" "mountpoint"

## Speed 

Theoretical speeds are roughly 1500 bytes/s write and 150,000 bytes/s read

Reason for this is discord-fs encodes files in base64(I'm planning to experiment with encoding multiple bytes into unicode characters in the future) and files span over multiple messages where each message can hold a maximum of 1500 bytes, we can only send one message at a time but retrieve 100  

## Behind the scenes

It's pretty simple. Each file has an inode which contains the various attributes. the most important ones being the message handle (message id right before data) and the amount of messages it spans over. the root inode is in the general channel topic and from there on out it can be nested to infinity, but the more you nest the more requests it takes to do stuff within that directory.