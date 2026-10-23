package rows

import "github.com/weiloon1234/Foundry-Go/internal/ormcore"

func (s Input[S]) correlationScope(*S) ormcore.VisibleScope {
	return ormcore.SourceVisibility(s.source)
}
func (s Aliased[A, M]) correlationScope(*Alias[A, M]) ormcore.VisibleScope {
	return ormcore.SourceVisibility(s.source)
}
func (s InnerJoined[L, R]) correlationScope(*Inner[L, R]) ormcore.VisibleScope {
	return ormcore.SourceVisibility(s.source)
}
func (s LeftJoined[L, R]) correlationScope(*Left[L, R]) ormcore.VisibleScope {
	return ormcore.SourceVisibility(s.source)
}
func (s RightJoined[L, R]) correlationScope(*Right[L, R]) ormcore.VisibleScope {
	return ormcore.SourceVisibility(s.source)
}
func (s FullJoined[L, R]) correlationScope(*Full[L, R]) ormcore.VisibleScope {
	return ormcore.SourceVisibility(s.source)
}
func (s CrossJoined[L, R]) correlationScope(*Cross[L, R]) ormcore.VisibleScope {
	return ormcore.SourceVisibility(s.source)
}
