package name

type FileResourceName struct {
	RecordName string
	FileName   string
}

func (n *FileResourceName) String() string {
	return n.RecordName + "/files/" + n.FileName
}
