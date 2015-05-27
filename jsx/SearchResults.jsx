/* jshint -W097,-W117 */
'use strict';

var utils = require('./utils.js');
var format = require('./format.js');

var TypeTitle = 1;
var TypeLine = 2;

// if true, show line number at the beginning of search results
var showLineNumbers = true;

/*
Format of search results:
{
  Term: "foo",
  Results: [
    {
      NoteIDStr: "1XRy",
      Items: [
        {
          Type: 1,
          LineNo: 5,
          HTML: 'foo<span class="s-r">bar</span>',
        }
      ]
    }
  ]
}
*/

var SearchResults = React.createClass({

  handleClick: function(noteIDStr, e) {
    e.preventDefault();
    this.props.searchResultSelectedCb(noteIDStr);
  },

  createNoResults: function(term) {
    return (
      <div id="search-results">
        <div className="box">
          <p>No results for {term}</p>
        </div>
      </div>
    );
  },

  createResultItem: function(noteID, i) {
    // Maybe: show line number
    var k = "" + noteID + "-" + i.Type + "-" + i.LineNo;
    console.log(k + i.HTML);
    var html = { __html: i.HTML };
    var cls = "search-result-item";
    if (i.Type == TypeTitle) {
      cls = "search-result-title-item";
    }
    var lineNo = i.LineNo + ":";
    if (i.LineNo == -1) {
      lineNo = "";
    }
    if (!showLineNumbers) {
      lineNo = "";
    }
    return (
      <div
        key={k}
        className={cls}
        >
        <span className="line-no">{lineNo}</span>
        <span dangerouslySetInnerHTML={html}></span>
      </div>
    );
  },

  createResultNote: function(o) {
    var noteID = o.NoteIDStr;
    var cb = this.handleClick.bind(this, noteID);
    var items = o.Items;
    var self = this;
    var children = items.map(function(i) {
      return self.createResultItem(noteID, i);
    });
    return (
      <div
        key={noteID}
        className="search-result-note"
        onClick={cb}>
        {children}
      </div>
    );
  },

  render: function() {
    var searchResults = this.props.searchResults;
    var results = searchResults.Results;
    if (!results || (results.length === 0)) {
      return this.createNoResults(searchResults.Term);
    }

    var self = this;
    var resultsHTML = results.map(function(o) {
      return self.createResultNote(o);
    });

    return (
      <div id="search-results">
        <div className="search-results-list">
          {resultsHTML}
        </div>
      </div>
    );
  }
});

module.exports = SearchResults;